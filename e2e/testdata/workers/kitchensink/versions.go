//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"

	"github.com/syumai/workers-go/exp/cloudflare/versions"
)

// versionMetadataBinding is the version_metadata binding name declared in
// wrangler.jsonc.
const versionMetadataBinding = "CF_VERSION_METADATA"

// versionMetadataResponse is the JSON shape of GET /versions: the fields
// versions.NewWorkerVersionMetadata decodes from the binding. Assertions
// live in the e2e test.
type versionMetadataResponse struct {
	ID        string `json:"id"`
	Tag       string `json:"tag"`
	Timestamp string `json:"timestamp"`
}

// handleVersions resolves the version_metadata binding and returns its
// id, tag, and timestamp. A resolve or encode error is returned as 500
// with the error text so the e2e test can see what wrangler dev produced.
func handleVersions(w http.ResponseWriter, r *http.Request) {
	meta, err := versions.NewWorkerVersionMetadata(versionMetadataBinding)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(versionMetadataResponse{
		ID:        meta.ID,
		Tag:       meta.Tag,
		Timestamp: meta.Timestamp,
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
