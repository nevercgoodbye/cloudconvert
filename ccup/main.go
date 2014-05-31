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
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tgulacsi/cloudconvert"
)

const ccAPIkeyEnvName = "CLOUDCONVERT_APIKEY"

func main() {
	flagAPIKey := flag.String("apikey", "", "api key (this, or "+ccAPIkeyEnvName+" env needed)")
	flagFromFormat := flag.String("fromfmt", "", "from format (optional, will from input file name)")
	flagToFormat := flag.String("tofmt", "", "to format - this or a second arg (destination filename) is needed")
	flagWaitDur := flag.Duration("wait", time.Second, "wait duration")
	flag.Parse()
	if flag.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "A filename to upload is needed.\n")
		os.Exit(1)
	}
	fromFile := flag.Arg(0)
	toFormat := *flagToFormat
	toFile := flag.Arg(1)
	if toFile == "" {
		if toFormat == "" {
			log.Fatalf("-tofmt or a destination filename (second arg) is needed!")
		}
		ext := filepath.Ext(fromFile)
		if ext == "" {
			toFile = fromFile + "." + toFormat
		} else {
			toFile = fromFile[:len(fromFile)-len(ext)] + "." + toFormat
		}
	}
	apiKey := *flagAPIKey
	if apiKey == "" {
		apiKey = os.Getenv(ccAPIkeyEnvName)
	}
	if apiKey == "" {
		log.Fatal("API key is needed!")
	}

	if err := convert(apiKey, fromFile, toFile, *flagFromFormat, toFormat, *flagWaitDur); err != nil {
		log.Fatal("ERROR: %v", err)
	}
}

func convert(apiKey, fromFile, toFile, fromFormat, toFormat string, wait time.Duration) error {
	c, err := cloudconvert.NewConversion(apiKey, fromFile, toFile, fromFormat, toFormat)
	if err != nil {
		return fmt.Errorf("NewConversion: %v", err)
	}
	log.Printf("process URL: %s", c.Process.URL)
	if err = c.Start(); err != nil {
		return fmt.Errorf("Start: %v", err)
	}
	log.Println("Uploaded.")
	if err = c.Wait(wait); err != nil {
		return err
	}
	log.Printf("Done.")
	return c.Save()
}
