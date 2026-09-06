//go:build js && wasm

package fetch

import (
	"context"
	"fmt"
	"reflect"
	"syscall/js"
	"testing"

	"github.com/syumai/workers-go/internal/jstest"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

func TestRedirectMode_IsValid(t *testing.T) {
	tests := map[string]struct {
		mode RedirectMode
		want bool
	}{
		"follow":  {mode: RedirectModeFollow, want: true},
		"error":   {mode: RedirectModeError, want: true},
		"manual":  {mode: RedirectModeManual, want: true},
		"empty":   {mode: RedirectMode(""), want: false},
		"invalid": {mode: RedirectMode("bogus"), want: false},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := tt.mode.IsValid(); got != tt.want {
				t.Errorf("IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRedirectMode_String(t *testing.T) {
	if got := RedirectModeManual.String(); got != "manual" {
		t.Errorf("String() = %q, want %q", got, "manual")
	}
}

func ExampleRedirectMode() {
	fmt.Println(RedirectModeFollow, RedirectModeError, RedirectModeManual)
	// Output: follow error manual
}

func TestRequestInit_ToJS(t *testing.T) {
	tests := map[string]struct {
		init *RequestInit
		want map[string]any // nil means undefined is expected
	}{
		"nil":             {init: nil, want: nil},
		"empty":           {init: &RequestInit{}, want: map[string]any{}},
		"redirect_follow": {init: &RequestInit{Redirect: RedirectModeFollow}, want: map[string]any{"redirect": "follow"}},
		"redirect_manual": {init: &RequestInit{Redirect: RedirectModeManual}, want: map[string]any{"redirect": "manual"}},
		"redirect_invalid_omitted": {
			init: &RequestInit{Redirect: RedirectMode("bogus")},
			want: map[string]any{},
		},
		"cf_nil": {init: &RequestInit{CF: nil}, want: map[string]any{}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := tt.init.ToJS()
			if tt.want == nil {
				if !got.IsUndefined() {
					t.Fatalf("ToJS() = %v, want undefined", got)
				}
				return
			}
			jstest.AssertObjectEqual(t, got, tt.want)
		})
	}
}

func TestNewIncomingBotManagementJsDetection(t *testing.T) {
	t.Run("undefined", func(t *testing.T) {
		if got := NewIncomingBotManagementJsDetection(js.Undefined()); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("value", func(t *testing.T) {
		v := js.ValueOf(map[string]any{"passed": true})
		got := NewIncomingBotManagementJsDetection(v)
		want := &IncomingBotManagementJsDetection{Passed: true}
		if got == nil || *got != *want {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

func TestNewIncomingBotManagement(t *testing.T) {
	t.Run("undefined", func(t *testing.T) {
		if got := NewIncomingBotManagement(js.Undefined()); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("value", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"corporateProxy": true,
			"verifiedBot":    false,
			"jsDetection":    map[string]any{"passed": true},
			"staticResource": true,
			"score":          42,
		})
		got := NewIncomingBotManagement(v)
		if got == nil {
			t.Fatal("got nil, want non-nil")
		}
		want := &IncomingBotManagement{
			CorporateProxy: true,
			VerifiedBot:    false,
			JsDetection:    &IncomingBotManagementJsDetection{Passed: true},
			StaticResource: true,
			Score:          42,
		}
		if got.CorporateProxy != want.CorporateProxy ||
			got.VerifiedBot != want.VerifiedBot ||
			got.StaticResource != want.StaticResource ||
			got.Score != want.Score {
			t.Fatalf("got %#v, want %#v", got, want)
		}
		if got.JsDetection == nil || *got.JsDetection != *want.JsDetection {
			t.Fatalf("JsDetection = %#v, want %#v", got.JsDetection, want.JsDetection)
		}
	})
}

func TestNewIncomingTLSClientAuth(t *testing.T) {
	t.Run("undefined", func(t *testing.T) {
		if got := NewIncomingTLSClientAuth(js.Undefined()); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("value", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"certIssuerDNLegacy": "issuer-legacy",
			"certSerial":         "1234",
		})
		got := NewIncomingTLSClientAuth(v)
		want := &IncomingTLSClientAuth{
			CertIssuerDNLegacy: "issuer-legacy",
			CertSerial:         "1234",
		}
		if got == nil || *got != *want {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

func TestNewIncomingTLSExportedAuthenticator(t *testing.T) {
	t.Run("undefined", func(t *testing.T) {
		if got := NewIncomingTLSExportedAuthenticator(js.Undefined()); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("value", func(t *testing.T) {
		v := js.ValueOf(map[string]any{
			"clientFinished":  "cf",
			"serverHandshake": "sh",
		})
		got := NewIncomingTLSExportedAuthenticator(v)
		want := &IncomingTLSExportedAuthenticator{
			ClientFinished:  "cf",
			ServerHandshake: "sh",
		}
		if got == nil || *got != *want {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	})
}

func TestNewIncomingProperties(t *testing.T) {
	t.Run("cf_missing", func(t *testing.T) {
		ctx := runtimecontext.New(context.Background(), js.ValueOf(map[string]any{}))
		_, err := NewIncomingProperties(ctx)
		if err == nil {
			t.Fatal("NewIncomingProperties() error = nil, want error")
		}
		if got, want := err.Error(), "runtime is not cloudflare"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("full", func(t *testing.T) {
		cf := map[string]any{
			"longitude":                "1.23",
			"latitude":                 "4.56",
			"tlsCipher":                "AEAD-AES256-GCM-SHA384",
			"continent":                "EU",
			"asn":                      123,
			"clientAcceptEncoding":     "gzip",
			"country":                  "FR",
			"tlsClientAuth":            map[string]any{"certSerial": "abc"},
			"tlsExportedAuthenticator": map[string]any{"clientFinished": "cf"},
			"tlsVersion":               "TLSv1.3",
			"colo":                     "CDG",
			"timezone":                 "Europe/Paris",
			"city":                     "Paris",
			"verifiedBotCategory":      "search-engine",
			"requestPriority":          "high",
			"httpProtocol":             "HTTP/2",
			"region":                   "IDF",
			"regionCode":               "IDF",
			"asOrganization":           "Example",
			"postalCode":               "75000",
			"botManagement": map[string]any{
				"corporateProxy": true,
				"verifiedBot":    false,
				"jsDetection":    map[string]any{"passed": true},
				"staticResource": false,
				"score":          99,
			},
		}
		ctx := runtimecontext.New(context.Background(), js.ValueOf(map[string]any{"cf": cf}))
		got, err := NewIncomingProperties(ctx)
		if err != nil {
			t.Fatalf("NewIncomingProperties() error = %v", err)
		}
		want := &IncomingProperties{
			Longitude:                "1.23",
			Latitude:                 "4.56",
			TLSCipher:                "AEAD-AES256-GCM-SHA384",
			Continent:                "EU",
			Asn:                      123,
			ClientAcceptEncoding:     "gzip",
			Country:                  "FR",
			TLSClientAuth:            &IncomingTLSClientAuth{CertSerial: "abc"},
			TLSExportedAuthenticator: &IncomingTLSExportedAuthenticator{ClientFinished: "cf"},
			TLSVersion:               "TLSv1.3",
			Colo:                     "CDG",
			Timezone:                 "Europe/Paris",
			City:                     "Paris",
			VerifiedBotCategory:      "search-engine",
			RequestPriority:          "high",
			HttpProtocol:             "HTTP/2",
			Region:                   "IDF",
			RegionCode:               "IDF",
			AsOrganization:           "Example",
			PostalCode:               "75000",
			BotManagement: &IncomingBotManagement{
				CorporateProxy: true,
				VerifiedBot:    false,
				JsDetection:    &IncomingBotManagementJsDetection{Passed: true},
				StaticResource: false,
				Score:          99,
			},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("NewIncomingProperties() = %#v, want %#v", got, want)
		}
	})

	// property.go reads TLSVersion, Colo, Timezone, HttpProtocol and
	// AsOrganization with js.Value.String() instead of jsutil.MaybeString,
	// so when those keys are absent from `cf` they come back as the
	// literal string "<undefined>" rather than "". This documents that
	// asymmetry (property.go:192-201); fixing it is a separate change.
	t.Run("optional_string_fields_undefined", func(t *testing.T) {
		ctx := runtimecontext.New(context.Background(), js.ValueOf(map[string]any{"cf": map[string]any{}}))
		got, err := NewIncomingProperties(ctx)
		if err != nil {
			t.Fatalf("NewIncomingProperties() error = %v", err)
		}
		for name, got := range map[string]string{
			"TLSVersion":     got.TLSVersion,
			"Colo":           got.Colo,
			"Timezone":       got.Timezone,
			"HttpProtocol":   got.HttpProtocol,
			"AsOrganization": got.AsOrganization,
		} {
			if got != "<undefined>" {
				t.Errorf("%s = %q, want %q", name, got, "<undefined>")
			}
		}
		if got.Longitude != "" {
			t.Errorf("Longitude = %q, want empty string", got.Longitude)
		}
	})
}

// TestNewIncomingProperties_fromContext exercises NewIncomingProperties the
// way handler_js.go actually sets it up: the trigger object is the incoming
// JS Request, which carries a `cf` property.
func TestNewIncomingProperties_fromContext(t *testing.T) {
	reqObj := jstest.Request(t, "GET", "https://example.com/", nil, nil)
	reqObj.Set("cf", js.ValueOf(map[string]any{"colo": "CDG"}))

	ctx := runtimecontext.New(context.Background(), reqObj)
	got, err := NewIncomingProperties(ctx)
	if err != nil {
		t.Fatalf("NewIncomingProperties() error = %v", err)
	}
	if got.Colo != "CDG" {
		t.Errorf("Colo = %q, want %q", got.Colo, "CDG")
	}
}
