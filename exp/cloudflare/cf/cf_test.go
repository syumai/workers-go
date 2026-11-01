//go:build js && wasm

package cf

import (
	"context"
	"net/http"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/runtimecontext"
)

// fakeCfProperties builds a JS object shaped enough like
// IncomingRequestCfProperties to exercise fields flattened in from several
// of its constituent interfaces (colo/asn from Base,
// hostMetadata from CloudflareForSaaSEnterprise, country from
// GeographicInformation) plus the int-overridden
// edgeRequestKeepAliveStatus.
func fakeCfProperties() js.Value {
	return js.ValueOf(map[string]any{
		"colo":                       "SJC",
		"asn":                        13335,
		"httpProtocol":               "HTTP/2",
		"requestPriority":            "",
		"tlsVersion":                 "TLSv1.3",
		"tlsCipher":                  "AEAD-AES128-GCM-SHA256",
		"edgeRequestKeepAliveStatus": 1,
		"clientTrustScore":           54,
		"hostMetadata":               "custom-metadata",
		"country":                    "US",
		"isEUCountry":                "",
	})
}

// TestFromJS verifies fields contributed by different constituent
// interfaces of the intersection all land on the flattened
// IncomingRequestCFProperties, and that the int-overridden
// edgeRequestKeepAliveStatus decodes correctly.
func TestFromJS(t *testing.T) {
	props, err := FromJS(fakeCfProperties())
	if err != nil {
		t.Fatalf("FromJS() failed: %v", err)
	}
	if props.Colo != "SJC" {
		t.Errorf("props.Colo = %q, want %q", props.Colo, "SJC")
	}
	if props.ASN != 13335 {
		t.Errorf("props.ASN = %v, want 13335", props.ASN)
	}
	if props.EdgeRequestKeepAliveStatus != 1 {
		t.Errorf("props.EdgeRequestKeepAliveStatus = %v, want 1", props.EdgeRequestKeepAliveStatus)
	}
	if got := props.HostMetadata.String(); got != "custom-metadata" {
		t.Errorf("props.HostMetadata = %q, want %q", got, "custom-metadata")
	}
	if props.Country != "US" {
		t.Errorf("props.Country = %q, want %q", props.Country, "US")
	}
	if props.ClientTrustScore != 54 {
		t.Errorf("props.ClientTrustScore = %v, want 54", props.ClientTrustScore)
	}
}

// TestFromJS_IsEUCountry coerces the shapes workerd actually produces.
// workers-types says the property is the string "1"; local workerd sends
// a boolean. Value.String on that boolean is not a usable country flag.
func TestFromJS_IsEUCountry(t *testing.T) {
	cases := map[string]struct {
		value any
		want  string
	}{
		"string 1":      {value: "1", want: "1"},
		"empty string":  {value: "", want: ""},
		"boolean true":  {value: true, want: "1"},
		"boolean false": {value: false, want: ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			v := fakeCfProperties()
			v.Set("isEUCountry", tc.value)
			props, err := FromJS(v)
			if err != nil {
				t.Fatalf("FromJS() failed: %v", err)
			}
			if props.IsEUCountry != tc.want {
				t.Errorf("props.IsEUCountry = %q, want %q", props.IsEUCountry, tc.want)
			}
		})
	}
}

// TestFromJS_OmitsClientTrustScore verifies a cf object without
// clientTrustScore (what workerd sends when Bot Management is not
// enabled) decodes instead of panicking in Value.Float.
func TestFromJS_OmitsClientTrustScore(t *testing.T) {
	v := fakeCfProperties()
	v.Delete("clientTrustScore")

	props, err := FromJS(v)
	if err != nil {
		t.Fatalf("FromJS() failed: %v", err)
	}
	if props.ClientTrustScore != 0 {
		t.Errorf("props.ClientTrustScore = %v, want 0", props.ClientTrustScore)
	}
	if props.Colo != "SJC" {
		t.Errorf("props.Colo = %q, want %q", props.Colo, "SJC")
	}
}

// TestFromRequest_MissingTriggerObj verifies FromRequest returns an error
// (rather than panicking) for a request that wasn't produced by
// workers.Serve, since it has no JS Request object attached to its
// context.
func TestFromRequest_MissingTriggerObj(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := FromRequest(req); err == nil {
		t.Fatal("FromRequest() succeeded, want an error for a request with no trigger object")
	}
}

// TestFromRequest_ReadsTriggerObjCf verifies FromRequest reads the `cf`
// property off the JS Request object workers.Serve stashes on the
// request's context (see internal/runtimecontext).
func TestFromRequest_ReadsTriggerObjCf(t *testing.T) {
	jsReq := js.ValueOf(map[string]any{"cf": fakeCfProperties()})
	ctx := runtimecontext.New(context.Background(), jsReq)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatal(err)
	}
	props, err := FromRequest(req)
	if err != nil {
		t.Fatalf("FromRequest() failed: %v", err)
	}
	if props.Colo != "SJC" {
		t.Errorf("props.Colo = %q, want %q", props.Colo, "SJC")
	}
}
