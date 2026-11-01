//go:build js && wasm

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/syumai/workers-go/exp/cloudflare/images"
)

// png32 is a 32×32 PNG. Info and the resize both start from these bytes
// so the test does not depend on fetching a remote image. 32×32 is large
// enough that a width-8 transform is a downscale.
var png32 = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x20, 0x00, 0x00, 0x00, 0x20,
	0x08, 0x02, 0x00, 0x00, 0x00, 0xfc, 0x18, 0xed, 0xa3, 0x00, 0x00, 0x00,
	0x34, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9c, 0xec, 0xcd, 0x31, 0x0d, 0x00,
	0x30, 0x0c, 0x03, 0x41, 0x0f, 0x19, 0x0a, 0xa5, 0xfc, 0x11, 0x56, 0xe5,
	0x10, 0x6f, 0xf7, 0xb2, 0xbc, 0xde, 0x24, 0xf7, 0x24, 0xbd, 0xcd, 0xbf,
	0x66, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x60, 0xab,
	0x17, 0x00, 0x00, 0xff, 0xff, 0x7f, 0x4a, 0x02, 0x99, 0xda, 0x35, 0x57,
	0x9a, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60,
	0x82,
}

// imagesResizeWidth is the width passed to ImageTransform. Small on purpose
// so the transformed PNG stays tiny.
const imagesResizeWidth = 8

// imagesResponse is the JSON shape of GET /images: Info on the source PNG,
// then the same bytes resized and described again.
type imagesResponse struct {
	Format        string  `json:"format"`
	Width         float64 `json:"width"`
	Height        float64 `json:"height"`
	FileSize      float64 `json:"fileSize"`
	ContentType   string  `json:"contentType"`
	ResizedBytes  int     `json:"resizedBytes"`
	ResizedWidth  float64 `json:"resizedWidth"`
	ResizedHeight float64 `json:"resizedHeight"`
}

// handleImages calls ImagesBinding.Info on an embedded 32×32 PNG, resizes it with
// Input/Transform/Output, and returns both sets of metadata. A binding or
// upstream failure is returned as 502 with the error text so the e2e test
// can skip when no Cloudflare account is configured.
func handleImages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	binding, err := images.NewImagesBinding("IMAGES")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	info, err := binding.Info(bytes.NewReader(png32), images.ImageInputOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	transformer, err := binding.Input(bytes.NewReader(png32), images.ImageInputOptions{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	transformer, err = transformer.Transform(images.ImageTransform{Width: imagesResizeWidth})
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
	if err := json.NewEncoder(w).Encode(imagesResponse{
		Format:        info.Format,
		Width:         info.Width,
		Height:        info.Height,
		FileSize:      info.FileSize,
		ContentType:   contentType,
		ResizedBytes:  len(resized),
		ResizedWidth:  resizedInfo.Width,
		ResizedHeight: resizedInfo.Height,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
