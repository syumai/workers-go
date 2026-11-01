//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// echoResponse mirrors testdata/workers/kitchensink/main.go's echoResponse.
type echoResponse struct {
	Method     string              `json:"method"`
	URL        string              `json:"url"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	RemoteAddr string              `json:"remoteAddr"`
	Host       string              `json:"host"`
}

// cfPropsResponse decodes the subset of testdata/workers/kitchensink/cf.go's
// GET /cf response (a JSON-encoded *fetch.IncomingProperties, whose fields
// carry no json tags, so field names are used as-is) that the e2e test
// needs.
type cfPropsResponse struct {
	Colo string
}

// cfExpResponse mirrors testdata/workers/kitchensink/cfexp.go. Geo and ASN
// values come from workerd's cf.json lookup for this machine's address.
// The test checks coarse fields that lookup fills anywhere, and only
// checks that finer fields decoded as plain strings: metroCode is a US
// DMA code, and city, region, regionCode, and postalCode are absent for
// some addresses.
type cfExpResponse struct {
	ASN                         float64 `json:"asn"`
	AsOrganization              string  `json:"asOrganization"`
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

// waitUntilPollTimeout bounds how long the waituntil/runs_after_response
// subtest polls GET /waituntil/result for the deferred write to land.
const waitUntilPollTimeout = 5 * time.Second

// TestKitchenSink starts a single `wrangler dev` instance for the
// kitchensink fixture and runs every kitchensink-backed check as a
// subtest, so the ~1-2s wrangler startup cost is paid once. KV-specific
// subtests live in kv_test.go; both files contribute t.Run cases to this
// same test function.
func TestKitchenSink(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	w := startWrangler(t, "kitchensink")

	t.Run("hello", func(t *testing.T) {
		resp, body := w.Get(t, "/healthz")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if body != "ok" {
			t.Errorf("body = %q, want %q", body, "ok")
		}
	})

	t.Run("echo/method_url", func(t *testing.T) {
		resp, body := w.Get(t, "/echo?x=1")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got echoResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal echo response %q: %v", body, err)
		}
		if got.Method != http.MethodGet {
			t.Errorf("method = %q, want %q", got.Method, http.MethodGet)
		}
		if !strings.HasSuffix(got.URL, "/echo?x=1") {
			t.Errorf("url = %q, want suffix %q", got.URL, "/echo?x=1")
		}
	})

	t.Run("echo/headers_multi_value", func(t *testing.T) {
		headers := http.Header{}
		headers.Add("X-Test", "a")
		headers.Add("X-Test", "b")
		resp, body := w.Do(t, http.MethodGet, "/echo", headers, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got echoResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal echo response %q: %v", body, err)
		}
		// The underlying fetch Headers object combines same-name headers
		// into a single "a, b" (comma-space) entry, and
		// internal/jshttp.ToHeader splits them back and trims the join
		// whitespace.
		want := []string{"a", "b"}
		if got := got.Headers["X-Test"]; !equalStringSlices(got, want) {
			t.Errorf("X-Test = %v, want %v", got, want)
		}
	})

	t.Run("echo/set_cookie_with_embedded_comma", func(t *testing.T) {
		// A Set-Cookie value that itself contains a comma (e.g. an
		// Expires date) must survive as a single header value:
		// internal/jshttp.ToHeader reads cookies via
		// Headers.getSetCookie() instead of splitting the combined
		// entries() value.
		const cookie = "a=1; Expires=Wed, 21 Oct 2015 07:28:00 GMT"
		headers := http.Header{"Set-Cookie": {cookie}}
		resp, body := w.Do(t, http.MethodGet, "/echo", headers, nil)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got echoResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal echo response %q: %v", body, err)
		}
		values := got.Headers["Set-Cookie"]
		if len(values) != 1 || values[0] != cookie {
			t.Errorf("Set-Cookie = %v, want [%q]", values, cookie)
		}
	})

	t.Run("echo/body", func(t *testing.T) {
		const wantBody = "hello from the e2e test"
		resp, body := w.Do(t, http.MethodPost, "/echo", nil, strings.NewReader(wantBody))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var got echoResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal echo response %q: %v", body, err)
		}
		if got.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", got.Method, http.MethodPost)
		}
		if got.Body != wantBody {
			t.Errorf("body = %q, want %q", got.Body, wantBody)
		}
	})

	t.Run("stream", func(t *testing.T) {
		resp, body := w.Get(t, "/stream?n=5")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		var want strings.Builder
		for i := 0; i < 5; i++ {
			want.WriteString("line " + strconv.Itoa(i) + "\n")
		}
		if body != want.String() {
			t.Errorf("body = %q, want %q", body, want.String())
		}
	})

	t.Run("fixed/content_length_preserved", func(t *testing.T) {
		const size = 12345
		resp, body := w.Get(t, "/fixed?size="+strconv.Itoa(size))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(size) {
			t.Errorf("Content-Length header = %q, want %q", got, strconv.Itoa(size))
		}
		if len(body) != size {
			t.Errorf("body length = %d, want %d", len(body), size)
		}
	})

	t.Run("env", func(t *testing.T) {
		resp, body := w.Get(t, "/env/GREETING")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		if body != "hello" {
			t.Errorf("body = %q, want %q", body, "hello")
		}
	})

	t.Run("cfexp/from_request", testCFExpFromRequest(w))
	t.Run("versions/metadata", testVersionsMetadata(w))
	t.Run("ratelimit/under_and_over_limit", testRateLimit(w))
	t.Run("images/info_resize", testImagesInfoResize(w))

	t.Run("cf/has_colo_or_error", func(t *testing.T) {
		resp, body := w.Get(t, "/cf")
		switch resp.StatusCode {
		case http.StatusOK:
			var props cfPropsResponse
			if err := json.Unmarshal([]byte(body), &props); err != nil {
				t.Fatalf("failed to unmarshal /cf response %q: %v", body, err)
			}
			if props.Colo == "" {
				t.Errorf("Colo is empty even though GET /cf returned 200 (body = %q)", body)
			}
		case http.StatusInternalServerError:
			// fetch.NewIncomingProperties (cloudflare/fetch/property.go)
			// returns this exact error when the trigger object has no "cf"
			// property, which is the case under some wrangler dev
			// configurations that don't populate request.cf locally.
			const wantErr = "runtime is not cloudflare"
			if got := strings.TrimSpace(body); got != wantErr {
				t.Errorf("error body = %q, want %q", got, wantErr)
			}
		default:
			t.Fatalf("status = %d, want %d or %d (body = %q)", resp.StatusCode, http.StatusOK, http.StatusInternalServerError, body)
		}
	})

	t.Run("waituntil/runs_after_response", func(t *testing.T) {
		resp, body := w.Get(t, "/waituntil")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /waituntil status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}

		deadline := time.Now().Add(waitUntilPollTimeout)
		var lastResp *http.Response
		var lastBody string
		for time.Now().Before(deadline) {
			lastResp, lastBody = w.Get(t, "/waituntil/result")
			if lastResp.StatusCode == http.StatusOK && lastBody == "done" {
				return // success
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("waitUntil task did not write %q to KV within %v; last GET /waituntil/result status = %d, body = %q", "done", waitUntilPollTimeout, lastResp.StatusCode, lastBody)
	})

	t.Run("kv/put_get", testKVPutGet(w))
	t.Run("kv/get_missing", testKVGetMissing(w))
	t.Run("kv/list_prefix", testKVListPrefix(w))
	t.Run("kv/delete", testKVDelete(w))

	t.Run("r2/put_get_roundtrip", testR2PutGetRoundtrip(w))
	t.Run("r2/head", testR2Head(w))
	t.Run("r2/get_missing", testR2GetMissing(w))
	t.Run("r2/delete", testR2Delete(w))
	t.Run("r2/list", testR2List(w))

	t.Run("d1/create_insert_query", testD1CreateInsertQuery(w))
	t.Run("d1/blob_roundtrip", testD1BlobRoundtrip(w))
	t.Run("d1/null_real_text", testD1NullRealText(w))

	t.Run("queue/roundtrip", testQueueRoundtrip(w))

	t.Run("cron/fires", testCronFires(w))
}

// testCFExpFromRequest checks exp/cloudflare/cf.FromRequest against the cf
// object wrangler dev (workerd) actually attaches to a local fetch.
// Fields that only exist on the production edge (Cloudflare for SaaS
// hostMetadata, a Bot Management clientTrustScore) are asserted absent.
// Coarse geo fields are asserted present but not equal to a particular
// colo: workerd fills them from a cf.json lookup for the machine's
// address. Finer fields (city, region, postalCode, metroCode) and
// clientTcpRtt are location- or network-dependent and may be empty or 0.
func testCFExpFromRequest(w *worker) func(*testing.T) {
	return func(t *testing.T) {
		resp, body := w.Get(t, "/cfexp")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d (body = %q)", resp.StatusCode, http.StatusOK, body)
		}
		var got cfExpResponse
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatalf("failed to unmarshal /cfexp response %q: %v", body, err)
		}

		// Stable for a local HTTP/1.1 request with no upstream.
		if got.HTTPProtocol != "HTTP/1.1" {
			t.Errorf("httpProtocol = %q, want %q", got.HTTPProtocol, "HTTP/1.1")
		}
		if got.RequestPriority != "" {
			t.Errorf("requestPriority = %q, want empty", got.RequestPriority)
		}
		if got.EdgeRequestKeepAliveStatus != 1 {
			t.Errorf("edgeRequestKeepAliveStatus = %d, want 1", got.EdgeRequestKeepAliveStatus)
		}
		// No Bot Management add-on locally, so the deprecated duplicate
		// score is omitted and decodes as 0. The botManagement object
		// itself is still present.
		if got.ClientTrustScore != 0 {
			t.Errorf("clientTrustScore = %v, want 0", got.ClientTrustScore)
		}
		if !got.HasBotManagement || !got.HasBotManagementScore {
			t.Errorf("botManagement missing (has=%v scorePresent=%v)", got.HasBotManagement, got.HasBotManagementScore)
		}
		if got.BotManagementScore <= 0 || got.BotManagementScore > 99 {
			t.Errorf("botManagement.score = %v, want in (0, 99]", got.BotManagementScore)
		}
		// workers-types: "1" in the EU, omitted otherwise. workerd sends a
		// boolean; FromJS must not surface the syscall/js debug string.
		if got.IsEUCountry != "" && got.IsEUCountry != "1" {
			t.Errorf("isEUCountry = %q, want \"\" or \"1\"", got.IsEUCountry)
		}
		if got.HasHostMetadata {
			t.Errorf("hostMetadata is set; local dev has no Cloudflare for SaaS metadata")
		}

		// Placeholder client-cert object workerd attaches when no mTLS
		// cert was presented.
		if !got.HasTLSClientAuth {
			t.Error("tlsClientAuth missing")
		}
		if got.TLSClientAuthCertPresented != "0" {
			t.Errorf("tlsClientAuth.certPresented = %q, want %q", got.TLSClientAuthCertPresented, "0")
		}
		if got.TLSClientAuthCertVerified != "NONE" {
			t.Errorf("tlsClientAuth.certVerified = %q, want %q", got.TLSClientAuthCertVerified, "NONE")
		}
		if !got.HasTLSExportedAuthenticator || got.TLSClientHandshake == "" {
			t.Errorf("tlsExportedAuthenticator.clientHandshake missing (has=%v handshake=%q)", got.HasTLSExportedAuthenticator, got.TLSClientHandshake)
		}

		// Present for any address workerd can resolve. Values themselves
		// still depend on where the test runs.
		nonEmpty := map[string]string{
			"colo":           got.Colo,
			"country":        got.Country,
			"continent":      got.Continent,
			"timezone":       got.Timezone,
			"latitude":       got.Latitude,
			"longitude":      got.Longitude,
			"asOrganization": got.AsOrganization,
			"tlsVersion":     got.TLSVersion,
			"tlsCipher":      got.TLSCipher,
		}
		for name, value := range nonEmpty {
			if value == "" {
				t.Errorf("%s is empty", name)
			}
		}
		// Not always known: metroCode is a US DMA code, and city, region,
		// regionCode, and postalCode are missing for some addresses. A
		// syscall/js debug string ("<undefined>", "<boolean: …>") would
		// mean the decoder mishandled the JS value.
		optionalGeo := map[string]string{
			"city":       got.City,
			"region":     got.Region,
			"regionCode": got.RegionCode,
			"postalCode": got.PostalCode,
			"metroCode":  got.MetroCode,
		}
		for name, value := range optionalGeo {
			if strings.HasPrefix(value, "<") {
				t.Errorf("%s = %q, want a plain string or empty", name, value)
			}
		}
		if got.ASN == 0 {
			t.Error("asn = 0, want the cf.json lookup's ASN")
		}
		// 0 is a real measurement (idle or unmeasured path), not a missing field.
		if got.ClientTcpRtt < 0 {
			t.Errorf("clientTcpRtt = %v, want >= 0", got.ClientTcpRtt)
		}
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
