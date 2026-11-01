// Command images reads and resizes an embedded PNG with the Images binding
// (exp/cloudflare/images).
//
// GET / returns Info for the source 1×1 PNG, then resizes it to width 8
// and returns Info for that result. No remote image is fetched.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/syumai/workers-go"
	"github.com/syumai/workers-go/exp/cloudflare/images"
)

// png1x1 is a 1×1 red PNG.
var png1x1 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xde, 0x00, 0x00, 0x00,
	0x0c, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
	0x00, 0x03, 0x01, 0x01, 0x00, 0xc9, 0xfe, 0x92, 0xef, 0x00, 0x00, 0x00,
	0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

const resizeWidth = 8

type response struct {
	Format        string  `json:"format"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	FileSize      float64 `json:"fileSize"`
	ContentType   string  `json:"contentType"`
	ResizedBytes  int     `json:"resizedBytes"`
	ResizedWidth  float64 `json:"resizedWidth"`
	ResizedHeight float64 `json:"resizedHeight"`
}

func main() {
	workers.Serve(http.HandlerFunc(handle))
}

func handle(w http.ResponseWriter, r *http.Request) {
	binding, err := images.NewImagesBinding("IMAGES")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	info, err := binding.Info(bytes.NewReader(png1x1), images.ImageInputOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	transformer, err := binding.Input(bytes.NewReader(png1x1), images.ImageInputOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	transformer, err = transformer.Transform(images.ImageTransform{Width: resizeWidth})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	result, err := transformer.Output(images.ImageOutputOptions{Format: "image/png"})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resp, err := result.HTTPResponse()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	resized, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	resizedInfo, err := binding.Info(bytes.NewReader(resized), images.ImageInputOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		contentType, err = result.ContentType()
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response{
		Format:        info.Format,
		Width:         info.Width,
		Height:        info.Height,
		FileSize:      info.FileSize,
		ContentType:   contentType,
		ResizedBytes:  len(resized),
		ResizedWidth:  resizedInfo.Width,
		ResizedHeight: resizedInfo.Height,
	}); err != nil {
		log.Println(err)
	}
}
