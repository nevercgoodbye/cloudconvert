// Copyright 2014 Tamás Gulácsi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//		http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package cloudconvert is a client for cloudconvert.org.
package cloudconvert

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/inconshreveable/log15.v2"
)

const CloudURLBase = "https://api.cloudconvert.org"

// Log is set to DiscardHandler by default.
// Set it to something else to see logs.
var Log = log15.New("lib", "cloudconvert")

func init() {
	Log.SetHandler(log15.DiscardHandler())
}

type History struct {
	ID        string         `json:"id"`
	Host      string         `json:"host"`
	Step      string         `json:"step"`
	StartTime string         `json:"starttime"`
	EndTime   string         `json:"endtime"`
	URL       string         `json:"url"`
	Status    StatusResponse `json:"-"`
}

// List returns the conversion history for the given API key.
func List(apiKey string) ([]History, error) {
	resp, err := http.Get(CloudURLBase + "/processes?apikey=" + apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var hist []History
	if err = json.NewDecoder(resp.Body).Decode(&hist); err != nil {
		return hist, err
	}
	for i, h := range hist {
		if strings.HasPrefix(h.URL, "//") {
			h.URL = "https:" + h.URL
			hist[i] = h
		}
		h.Status, err = Process{URL: h.URL}.Status()
		if err != nil {
			Log.Warn("Getting process status.", "URL", h.URL, "error", err)
			continue
		}
		hist[i] = h
	}
	return hist, err
}

type ConversionType struct {
	InputFormat      string `json:"inputformat"`
	OutputFormat     string `json:"outputformat"`
	Converter        string `json:"converter"`
	ConverterOptions map[string]string
}

// ConversionTypes returns a list with all the possible conversions,
// and conversion specific options.
//
// Either input params can be zero value ("") for no filtering.
func ConversionTypes(inputFormat, outputFormat string) ([]ConversionType, error) {
	var v url.Values
	if inputFormat != "" {
		v.Set("inputformat", inputFormat)
	}
	if outputFormat != "" {
		v.Set("outputformat", outputFormat)
	}
	resp, err := http.Get(CloudURLBase + "/conversiontypes?" + v.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var m []ConversionType
	err = json.NewDecoder(resp.Body).Decode(&m)
	return m, err
}

// IsPossible checks whether the conversion between from ant to is possible.
func IsPossible(from, to string) (bool, error) {
	if from == "" || to == "" {
		return false, nil
	}

	m, err := ConversionTypes(from, to)
	if err != nil {
		return false, err
	}
	return len(m) > 0, nil
}

type Process struct {
	URL         string `json:"url"`
	Error       string `json:"error"`
	DownloadURL string `json:"-"`
}

// NewProcess inits the process in the remote server, returns the process' url.
func NewProcess(apiKey, inputFormat, outputFormat string) (Process, error) {
	v := url.Values{"inputformat": {inputFormat}, "outputformat": {outputFormat}, "apikey": {apiKey}}
	resp, err := http.Get(CloudURLBase + "/process?" + v.Encode())
	if err != nil {
		return Process{}, err
	}
	if resp.Body != nil {
		defer resp.Body.Close()
	}

	var p Process
	err = json.NewDecoder(resp.Body).Decode(&p)
	if err == nil && strings.HasPrefix(p.URL, "//") {
		p.URL = "https:" + p.URL
	}
	return p, err
}

// ID returns the process ID.
func (p Process) ID() string {
	i := strings.LastIndex(p.URL, "/")
	if i < 0 {
		return p.URL
	}
	return p.URL[i+1:]
}

// UploadFile uploads a file, requesting the output format.
type Options struct {
	Email          bool
	Output         string
	Callback       string
	ConversionOpts map[string]string
}

// UploadFile uploads the file with outFormat, and opts options are optional.
func (p Process) UploadFile(file, outFormat string, opts *Options) (StatusResponse, error) {
	r, w := io.Pipe()
	bw := bufio.NewWriter(w)
	mw := multipart.NewWriter(bw)
	var sr StatusResponse
	var o Options
	if opts != nil {
		o = *opts
	}
	omap := make(map[string]string, 5+len(opts.ConversionOpts))
	omap["input"] = "upload"
	omap["outputformat"] = outFormat
	omap["output"] = o.Output
	omap["callback"] = o.Callback
	if o.Email {
		omap["email"] = "1"
	}
	for k, v := range o.ConversionOpts {
		omap["options["+k+"]"] = v
	}
	if err := mwSetMap(mw, omap); err != nil {
		return sr, err
	}
	pw, err := mw.CreateFormFile("file", filepath.Base(file))
	if err != nil {
		return sr, err
	}
	f, err := os.Open(file)
	if err != nil {
		return sr, err
	}
	defer f.Close()
	go func() {
		defer w.Close()
		defer bw.Flush()
		defer mw.Close()
		Log.Debug("Uploading", "file", f.Name())
		n, e := io.Copy(pw, f)
		if e == nil {
			Log.Debug("Uploaded", "bytes", n)
		} else {
			Log.Error("Uploaded", "bytes", n, "error", e)
		}
	}()
	resp, err := http.Post(p.URL, mw.FormDataContentType(), r)
	if err == nil && resp.Body != nil {
		defer resp.Body.Close()
		if err = json.NewDecoder(resp.Body).Decode(&sr); err != nil {
			Log.Error("UploadFile", "error", err)
		}
	}
	return sr, err
}

func mwSet(mw *multipart.Writer, key, value string) error {
	pw, err := mw.CreateFormField(key)
	if err != nil {
		return err
	}
	if _, err = pw.Write([]byte(value)); err != nil {
		return err
	}
	return nil
}

func mwSetMap(mw *multipart.Writer, omap map[string]string) error {
	for k, v := range omap {
		if v == "" {
			continue
		}
		if err := mwSet(mw, k, v); err != nil {
			return err
		}
	}
	return nil
}

type StatusResponse struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Percent   json.RawMessage `json:"percent"` // either string or float32
	Message   string          `json:"message"`
	Step      string          `json:"step"`
	StartTime int64           `json:"starttime"`
	EndTime   int64           `json:"endtime"`
	Expire    int64           `json:"expire"`
	Input     StatusInput     `json:"input"`
	Output    StatusOutput    `json:"output"`
	Converter StatusConverter `json:"converter"`
}

type StatusInput struct {
	Type     string `json:"type"`
	FileName string `json:"filename"`
	Size     int64  `json:"size"`
	Name     string `json:"name"`
	Ext      string `json:"ext"`
}

type StatusOutput struct {
	FileName  string   `json:"filename"`
	Ext       string   `json:"ext"`
	Files     []string `json:"files"`
	Size      int64    `json:"size"`
	URL       string   `json:"url"`
	Downloads int      `json:"downloads"`
}

type StatusConverter struct {
	Format   string            `json:"format"`
	Type     string            `json:"type"`
	Options  map[string]string `json:"options"`
	Duration float64           `json:"duration"`
}

// Status returns the conversion process' status info.
func (p Process) Status() (StatusResponse, error) {
	resp, err := http.Get(p.URL)
	if err != nil {
		return StatusResponse{}, err
	}
	defer resp.Body.Close()
	var s StatusResponse
	err = json.NewDecoder(resp.Body).Decode(&s)
	if err == nil && strings.HasPrefix(s.Output.URL, "//") {
		s.Output.URL = p.URL[:strings.IndexByte(p.URL, ':')+1] + s.Output.URL
	}
	return s, err
}

// Download downloads the output file.
func (p Process) Download() (io.ReadCloser, error) {
	dlURL := p.DownloadURL
	if dlURL == "" {
		s, err := p.Status()
		if err != nil {
			return nil, err
		}
		if s.Step == "error" {
			return nil, errors.New(s.Message)
		}
		dlURL = s.Output.URL
	}
	Log.Debug("Begin downloading.", "URL", dlURL)
	resp, err := http.Get(dlURL)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// Cancel cancels the conversion method.
// Currently there is no way to resume.
func (p Process) Cancel() error {
	resp, err := http.Get(p.URL + "/cancel")
	if err == nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return err
}

// Delete deletes the files of the conversion process.
func (p Process) Delete() error {
	resp, err := http.Get(p.URL + "/delete")
	if err == nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	return err
}

// Save saves the Process' output to the given file.
func (p Process) Save(toFile string) error {
	d, err := p.Download()
	Log.Info("Save", "error", err)
	if err != nil {
		return err
	}
	defer d.Close()
	f, err := os.Create(toFile)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, d)
	return err
}

func getFormat(fileName string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return ""
	}
	return ext[1:] // cut starting dot
}

type Conversion struct {
	Process
	fromFile, toFile, toFormat string
}

// NewConversion prepares the conversion.
func NewConversion(apiKey, fromFile, toFile, fromFormat, toFormat string) (Conversion, error) {
	// If this doesn't get resolved right, user has to provide the correct ones.
	if fromFormat == "" {
		fromFormat = getFormat(fromFile)
	}
	if toFormat == "" {
		toFormat = getFormat(toFile)
	}

	p, err := NewProcess(apiKey, fromFormat, toFormat)
	if err != nil {
		return Conversion{}, err
	}
	if p.Error != "" {
		return Conversion{}, errors.New(p.Error)
	}
	return Conversion{Process: p, fromFile: fromFile, toFile: toFile, toFormat: toFormat}, nil
}

// Start uploads the previously given file, hence starting the conversion process.
// opts can be nil for default download file.
func (c Conversion) Start(opts *Options) error {
	_, err := c.Process.UploadFile(c.fromFile, c.toFormat, opts)
	return err
}

// Wait blocks until the process' status (step) changes to "finished";
// then returns nil, or the error if it has changed to "error".
func (c Conversion) Wait() error {
	errcnt := 0
	for {
		s, err := c.Process.Status()
		if err != nil {
			Log.Error("wait checking status", "error", err)
			errcnt++
			if errcnt > 3 {
				return err
			}
			time.Sleep(10 * time.Second)
			continue
		}
		Log.Debug("wait", "status", s.Step)
		switch s.Step {
		case "error":
			return errors.New(s.Message)
		case "finished":
			return nil
		}
		wait := time.Second
		perc := strings.Trim(string(s.Percent), `"`)
		if perc != "" {
			percent, err := strconv.ParseFloat(perc, 32)
			if err != nil || percent < 0 || percent > 100 {
				Log.Warn("Wait parse percent", "percent", perc, "error", err)
			} else {
				// elapsed time = percent, remaining time = (100% - percent)
				// => full time = 100 * elapsed_time / percent
				elapsed := float64(time.Since(time.Unix(s.StartTime, 0)))
				wait = time.Duration(elapsed / percent * (100.0 - percent) / 2.0)
				Log.Debug("Wait", "percent", perc, "starttime", s.StartTime, "elapsed", elapsed, "wait", wait)
				if wait < time.Second {
					wait = time.Second
				} else if wait > time.Minute {
					wait = time.Minute
				}
			}
		}
		time.Sleep(wait)
	}
	return nil
}

// Save saves the output with the designated filename and extension.
func (c Conversion) Save() error {
	return c.Process.Save(c.toFile)
}
