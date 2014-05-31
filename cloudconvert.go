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
	"time"
)

const CloudURLBase = "https://api.cloudconvert.org"

type History struct {
	ID        string `json:"id"`
	Host      string `json:"host"`
	Step      string `json:"step"`
	StartTime string `json:"starttime"`
	EndTime   string `json:"endtime"`
	URL       string `json:"url"`
}

// List returns the conversion history for the given API key.
func List(apiKey string) ([]History, error) {
	resp, err := http.Get(CloudURLBase + "/processes?apikey=" + apiKey)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var hist []History
	err = json.NewDecoder(resp.Body).Decode(&hist)
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
	URL   string `json:"url"`
	Error string `json:"error"`
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
	if err == nil && p.URL != "" && p.URL[0] == '/' {
		p.URL = "https:" + p.URL
	}
	return p, err
}

// UploadFile uploads a file, requesting the output format.
func (p Process) UploadFile(file, outFormat string) error {
	r, w := io.Pipe()
	bw := bufio.NewWriter(w)
	mw := multipart.NewWriter(bw)
	pw, err := mw.CreateFormField("input")
	if err != nil {
		return err
	}
	if _, err = pw.Write([]byte("upload")); err != nil {
		return err
	}
	if pw, err = mw.CreateFormField("outputformat"); err != nil {
		return err
	}
	if _, err = pw.Write([]byte(outFormat)); err != nil {
		return err
	}
	if pw, err = mw.CreateFormFile("file", file); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	go func() {
		defer bw.Flush()
		defer mw.Close()
		_, _ = io.Copy(pw, f)
	}()
	resp, err := http.Post(p.URL, mw.FormDataContentType(), r)
	if err == nil && resp.Body != nil {
		resp.Body.Close()
	}
	return err
}

type StatusResponse struct {
	ID        string          `json:"id"`
	URL       string          `json:"url"`
	Percent   string          `json:"percent"`
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
	return s, err
}

// Download downloads the output file.
func (p Process) Download() (io.ReadCloser, error) {
	s, err := p.Status()
	if err != nil {
		return nil, err
	}
	if s.Step == "error" {
		return nil, errors.New(s.Message)
	}
	resp, err := http.Get(s.Output.URL)
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
func (c Conversion) Start() error {
	return c.Process.UploadFile(c.fromFile, c.toFormat)
}

// Wait blocks until the process' status (step) changes to "finished";
// then returns nil, or the error if it has changed to "error".
// The default checkInterval is 30 seconds.
func (c Conversion) Wait(checkInterval time.Duration) error {
	errcnt := 0
	t := time.NewTimer(checkInterval)
	defer t.Stop()
	for _ = range t.C {
		s, err := c.Process.Status()
		if err != nil {
			errcnt++
			if errcnt > 3 {
				return err
			}
			continue
		}
		switch s.Step {
		case "error":
			return errors.New(s.Message)
		case "finished":
			return nil
		}
	}
	return nil
}

// Save saves the output with the designated filename and extension.
func (c Conversion) Save() error {
	d, err := c.Process.Download()
	if err != nil {
		return err
	}
	defer d.Close()
	f, err := os.Create(c.toFile)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, d)
	return err
}
