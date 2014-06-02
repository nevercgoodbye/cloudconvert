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

// Package main of ccup is a simple uploading client for cloduconvert.org
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/tgulacsi/cloudconvert"
	"gopkg.in/inconshreveable/log15.v2"
)

const ccAPIkeyEnvName = "CLOUDCONVERT_APIKEY"

var oldConversions = make(map[string]string)

func main() {
	flagVerbose := flag.Bool("v", false, "verbose logging")
	flagAPIKey := flag.String("apikey", "", "api key (this, or "+ccAPIkeyEnvName+" env needed)")
	flagFromFormat := flag.String("fromfmt", "", "from format (optional, will from input file name)")
	flagToFormat := flag.String("tofmt", "", "to format - this or a second arg (destination filename) is needed")
	flagMulti := flag.Bool("multi", false, "arguments are input files - concurrent uplad")
	flagOutput := flag.String("output", "", "output to dropbox, googledrive or s3 (default: file)")
	flag.Parse()

	log15.Root().SetHandler(log15.StderrHandler)
	if !*flagVerbose {
		log15.Root().SetHandler(log15.LvlFilterHandler(log15.LvlInfo, log15.StderrHandler))
	}
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "A filename to upload is needed.\n")
		os.Exit(1)
	}
	apiKey := *flagAPIKey
	if apiKey == "" {
		apiKey = os.Getenv(ccAPIkeyEnvName)
	}
	if apiKey == "" {
		log15.Crit("API key is needed!")
		os.Exit(3)
	}
	if history, err := cloudconvert.List(apiKey); err != nil {
		log15.Error("Downloading history.", "error", err)
	} else {
		for _, h := range history {
			if h.Step != "finished" ||
				h.Status.Output.URL == "" || h.Status.Output.FileName == "" ||
				h.Status.Input.FileName == "" {
				continue
			}
			log15.Debug("old", "input", h.Status.Input.FileName, "output", h.Status.Output.URL)
			key := strings.ToLower(filepath.Base(h.Status.Input.FileName))
			oldConversions[key] = h.Status.Output.URL
			key = strings.Replace(key, " ", string([]rune{os.PathSeparator}), -1)
			oldConversions[filepath.Base(key)] = h.Status.Output.URL
		}
		log15.Debug("History", "old", oldConversions)
	}
	toFormat := *flagToFormat
	opts := &cloudconvert.Options{Output: *flagOutput}

	if !*flagMulti {
		fromFile := flag.Arg(0)
		toFile := flag.Arg(1)
		if toFile == "" {
			if toFormat == "" {
				log15.Crit("-tofmt or a destination filename (second arg) is needed!")
				os.Exit(2)
			}
			toFile = changeExt(fromFile, toFormat)
		}

		if err := convert(apiKey, fromFile, toFile, *flagFromFormat, toFormat, opts); err != nil {
			log15.Crit("ERROR", "error", err)
			os.Exit(4)
		}
		return
	}

	if toFormat == "" {
		log15.Crit("-tofmt is needed with -multi!")
		os.Exit(2)
	}
	conc := make(chan struct{}, 5)
	for i := 0; i < cap(conc); i++ {
		conc <- struct{}{}
	}
	var wg sync.WaitGroup
	for _, fromFile := range flag.Args() {
		wg.Add(1)
		go func(fromFile string) {
			defer wg.Done()
			token := <-conc
			toFile := changeExt(fromFile, toFormat)
			if err := convert(apiKey, fromFile, toFile, *flagFromFormat, toFormat, opts); err != nil {
				log15.Error("ERROR", "file", fromFile, "error", err)
			} else {
				log15.Info("converted", "file", fromFile)
			}
			conc <- token
		}(fromFile)
	}
	wg.Wait()
	return
}

func convert(apiKey, fromFile, toFile, fromFormat, toFormat string, opts *cloudconvert.Options) error {
	log15.Debug("Search old conversions", "file", fromFile)
	fromFileL := strings.ToLower(fromFile)
	oldURL, ok := oldConversions[fromFileL]
	if !ok {
		var key string
		for key, oldURL = range oldConversions {
			log15.Debug("?", "fromFile", fromFile, "key", key)
			if strings.HasSuffix(fromFile, key) {
				ok = true
				break
			}
		}
	}
	if ok {
		log15.Info("File already converted.", "file", fromFile, "URL", oldURL)
		return cloudconvert.Process{DownloadURL: oldURL}.Save(toFile)
	}
	c, err := cloudconvert.NewConversion(apiKey, fromFile, toFile, fromFormat, toFormat)
	if err != nil {
		return fmt.Errorf("NewConversion: %v", err)
	}
	log15.Info("process", "URL", c.Process.URL)
	if err = c.Start(opts); err != nil {
		return fmt.Errorf("Start: %v", err)
	}
	log15.Info("Uploaded.")
	if opts != nil && opts.Output != "" {
		log15.Warn("Output is not file, skip saving!", "output", opts.Output)
		return nil
	}
	if err = c.Wait(); err != nil {
		return err
	}
	log15.Info("Done.")
	return c.Save()
}

func changeExt(fileName, newExt string) string {
	ext := filepath.Ext(fileName)
	if ext == "" {
		return fileName + "." + newExt
	}
	return fileName[:len(fileName)-len(ext)] + "." + newExt
}
