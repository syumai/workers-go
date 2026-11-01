//go:build js && wasm

package main

import (
	"encoding/json"
	"net/http"
	"syscall/js"

	"github.com/syumai/workers-go/exp/cloudflare/cf"
)

// cfExpResponse is the JSON shape of GET /cfexp: the scalar fields of
// exp/cloudflare/cf.IncomingRequestCFProperties, plus presence flags for
// the js.Value fields (botManagement, hostMetadata, tlsClientAuth) and the
// nested tlsExportedAuthenticator, which do not JSON-encode usefully on
// their own. Assertions live in the e2e test.
type cfExpResponse struct {
	ASN                         float64 `json:"asn"`
	AsOrganization              string  `json:"asOrganization"`
	ClientAcceptEncoding        string  `json:"clientAcceptEncoding"`
	ClientTcpRtt                float64 `json:"clientTcpRtt"`
	Colo                        string  `json:"colo"`
	EdgeRequestKeepAliveStatus  int     `json:"edgeRequestKeepAliveStatus"`
	HTTPProtocol                string  `json:"httpProtocol"`
	RequestPriority             string  `json:"requestPriority"`
	TLSVersion                  string  `json:"tlsVersion"`
	TLSCipher                   string  `json:"tlsCipher"`
	ClientTrustScore            float64 `json:"clientTrustScore"`
	Country                     string  `json:"country"`
	IsEUCountry                 string  `json:"isEUCountry"`
	Continent                   string  `json:"continent"`
	City                        string  `json:"city"`
	PostalCode                  string  `json:"postalCode"`
	Latitude                    string  `json:"latitude"`
	Longitude                   string  `json:"longitude"`
	Timezone                    string  `json:"timezone"`
	Region                      string  `json:"region"`
	RegionCode                  string  `json:"regionCode"`
	MetroCode                   string  `json:"metroCode"`
	HasTLSExportedAuthenticator bool    `json:"hasTlsExportedAuthenticator"`
	TLSClientHandshake          string  `json:"tlsClientHandshake"`
	HasBotManagement            bool    `json:"hasBotManagement"`
	HasBotManagementScore       bool    `json:"hasBotManagementScore"`
	BotManagementScore          float64 `json:"botManagementScore"`
	HasHostMetadata             bool    `json:"hasHostMetadata"`
	HasTLSClientAuth            bool    `json:"hasTlsClientAuth"`
	TLSClientAuthCertPresented  string  `json:"tlsClientAuthCertPresented"`
	TLSClientAuthCertVerified   string  `json:"tlsClientAuthCertVerified"`
}

// handleCFExp decodes the incoming request's Cloudflare edge metadata via
// exp/cloudflare/cf.FromRequest (the generated IncomingRequestCFProperties
// path) and returns it as JSON. A decode error is returned as 500 with the
// error text so the e2e test can see what wrangler dev actually produced.
func handleCFExp(w http.ResponseWriter, r *http.Request) {
	props, err := cf.FromRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := cfExpResponse{
		ASN:                         props.ASN,
		AsOrganization:              props.AsOrganization,
		ClientAcceptEncoding:        props.ClientAcceptEncoding,
		ClientTcpRtt:                props.ClientTcpRtt,
		Colo:                        props.Colo,
		EdgeRequestKeepAliveStatus:  props.EdgeRequestKeepAliveStatus,
		HTTPProtocol:                props.HTTPProtocol,
		RequestPriority:             props.RequestPriority,
		TLSVersion:                  props.TLSVersion,
		TLSCipher:                   props.TLSCipher,
		ClientTrustScore:            props.ClientTrustScore,
		Country:                     props.Country,
		IsEUCountry:                 props.IsEUCountry,
		Continent:                   string(props.Continent),
		City:                        props.City,
		PostalCode:                  props.PostalCode,
		Latitude:                    props.Latitude,
		Longitude:                   props.Longitude,
		Timezone:                    props.Timezone,
		Region:                      props.Region,
		RegionCode:                  props.RegionCode,
		MetroCode:                   props.MetroCode,
		HasTLSExportedAuthenticator: props.TLSExportedAuthenticator != nil,
		HasBotManagement:            jsValuePresent(props.BotManagement),
		HasHostMetadata:             jsValuePresent(props.HostMetadata),
		HasTLSClientAuth:            jsValuePresent(props.TLSClientAuth),
	}
	if props.TLSExportedAuthenticator != nil {
		resp.TLSClientHandshake = props.TLSExportedAuthenticator.ClientHandshake
	}
	if jsValuePresent(props.BotManagement) {
		if s := props.BotManagement.Get("score"); !s.IsUndefined() && !s.IsNull() {
			resp.HasBotManagementScore = true
			resp.BotManagementScore = s.Float()
		}
	}
	if jsValuePresent(props.TLSClientAuth) {
		resp.TLSClientAuthCertPresented = props.TLSClientAuth.Get("certPresented").String()
		resp.TLSClientAuthCertVerified = props.TLSClientAuth.Get("certVerified").String()
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func jsValuePresent(v js.Value) bool {
	return !v.IsUndefined() && !v.IsNull()
}
