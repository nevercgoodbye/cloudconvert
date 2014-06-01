# CloudConvert Go client #
This package is a Go client for cloudconvert.org - converting files/videos/audio.

## Install ##
`go get github.com/tgulacsi/cloudconvert`

## Example client ##
`go get github.com/tgulacsi/cloudconvert/ccup`

This can be used as 

`ccup -apikey="YOUR API KEY" "file to convert" "destination file name"`

or

`ccup -apikey="YOUR API KEY" -multi -tofmt=webm *.MTS`
