package fetch

import (
	"context"
	"errors"
	"syscall/js"

	"github.com/syumai/workers-go/internal/jsutil"
	"github.com/syumai/workers-go/internal/runtimecontext"
)

// RedirectMode represents the redirect mode of a fetch() request.
type RedirectMode string

var (
	RedirectModeFollow RedirectMode = "follow"
	RedirectModeError  RedirectMode = "error"
	RedirectModeManual RedirectMode = "manual"
)

func (mode RedirectMode) IsValid() bool {
	return mode == RedirectModeFollow || mode == RedirectModeError || mode == RedirectModeManual
}

func (mode RedirectMode) String() string {
	return string(mode)
}

// RequestInit represents the options passed to a fetch() request.
type RequestInit struct {
	CF       *RequestInitCF
	Redirect RedirectMode
}

// ToJS converts RequestInit to JS object.
func (init *RequestInit) ToJS() js.Value {
	if init == nil {
		return js.Undefined()
	}
	obj := jsutil.NewObject()
	if init.Redirect.IsValid() {
		obj.Set("redirect", init.Redirect.String())
	}
	return obj
}

// RequestInitCF represents the Cloudflare-specific options passed to a fetch() request.
type RequestInitCF struct {
	/* TODO: implement */
}

type IncomingBotManagementJsDetection struct {
	Passed bool
}

func NewIncomingBotManagementJsDetection(cf js.Value) *IncomingBotManagementJsDetection {
	if cf.IsUndefined() {
		return nil
	}
	return &IncomingBotManagementJsDetection{
		Passed: cf.Get("passed").Bool(),
	}
}

type IncomingBotManagement struct {
	CorporateProxy bool
	VerifiedBot    bool
	JsDetection    *IncomingBotManagementJsDetection
	StaticResource bool
	Score          int
}

func NewIncomingBotManagement(cf js.Value) *IncomingBotManagement {
	if cf.IsUndefined() {
		return nil
	}
	return &IncomingBotManagement{
		CorporateProxy: cf.Get("corporateProxy").Bool(),
		VerifiedBot:    cf.Get("verifiedBot").Bool(),
		JsDetection:    NewIncomingBotManagementJsDetection(cf.Get("jsDetection")),
		StaticResource: cf.Get("staticResource").Bool(),
		Score:          cf.Get("score").Int(),
	}
}

type IncomingTLSClientAuth struct {
	CertIssuerDNLegacy    string
	CertIssuerSKI         string
	CertSubjectDNRFC2253  string
	CertSubjectDNLegacy   string
	CertFingerprintSHA256 string
	CertNotBefore         string
	CertSKI               string
	CertSerial            string
	CertIssuerDN          string
	CertVerified          string
	CertNotAfter          string
	CertSubjectDN         string
	CertPresented         string
	CertRevoked           string
	CertIssuerSerial      string
	CertIssuerDNRFC2253   string
	CertFingerprintSHA1   string
}

func NewIncomingTLSClientAuth(cf js.Value) *IncomingTLSClientAuth {
	if cf.IsUndefined() {
		return nil
	}
	return &IncomingTLSClientAuth{
		CertIssuerDNLegacy:    jsutil.MaybeString(cf.Get("certIssuerDNLegacy")),
		CertIssuerSKI:         jsutil.MaybeString(cf.Get("certIssuerSKI")),
		CertSubjectDNRFC2253:  jsutil.MaybeString(cf.Get("certSubjectDNRFC2253")),
		CertSubjectDNLegacy:   jsutil.MaybeString(cf.Get("certSubjectDNLegacy")),
		CertFingerprintSHA256: jsutil.MaybeString(cf.Get("certFingerprintSHA256")),
		CertNotBefore:         jsutil.MaybeString(cf.Get("certNotBefore")),
		CertSKI:               jsutil.MaybeString(cf.Get("certSKI")),
		CertSerial:            jsutil.MaybeString(cf.Get("certSerial")),
		CertIssuerDN:          jsutil.MaybeString(cf.Get("certIssuerDN")),
		CertVerified:          jsutil.MaybeString(cf.Get("certVerified")),
		CertNotAfter:          jsutil.MaybeString(cf.Get("certNotAfter")),
		CertSubjectDN:         jsutil.MaybeString(cf.Get("certSubjectDN")),
		CertPresented:         jsutil.MaybeString(cf.Get("certPresented")),
		CertRevoked:           jsutil.MaybeString(cf.Get("certRevoked")),
		CertIssuerSerial:      jsutil.MaybeString(cf.Get("certIssuerSerial")),
		CertIssuerDNRFC2253:   jsutil.MaybeString(cf.Get("certIssuerDNRFC2253")),
		CertFingerprintSHA1:   jsutil.MaybeString(cf.Get("certFingerprintSHA1")),
	}
}

type IncomingTLSExportedAuthenticator struct {
	ClientFinished  string
	ClientHandshake string
	ServerHandshake string
	ServerFinished  string
}

func NewIncomingTLSExportedAuthenticator(cf js.Value) *IncomingTLSExportedAuthenticator {
	if cf.IsUndefined() {
		return nil
	}
	return &IncomingTLSExportedAuthenticator{
		ClientFinished:  jsutil.MaybeString(cf.Get("clientFinished")),
		ClientHandshake: jsutil.MaybeString(cf.Get("clientHandshake")),
		ServerHandshake: jsutil.MaybeString(cf.Get("serverHandshake")),
		ServerFinished:  jsutil.MaybeString(cf.Get("serverFinished")),
	}
}

type IncomingProperties struct {
	Longitude                string
	Latitude                 string
	TLSCipher                string
	Continent                string
	Asn                      int
	ClientAcceptEncoding     string
	Country                  string
	TLSClientAuth            *IncomingTLSClientAuth
	TLSExportedAuthenticator *IncomingTLSExportedAuthenticator
	TLSVersion               string
	Colo                     string
	Timezone                 string
	City                     string
	VerifiedBotCategory      string
	// EdgeRequestKeepAliveStatus int
	RequestPriority string
	HttpProtocol    string
	Region          string
	RegionCode      string
	AsOrganization  string
	PostalCode      string
	BotManagement   *IncomingBotManagement
}

func NewIncomingProperties(ctx context.Context) (*IncomingProperties, error) {
	obj := runtimecontext.MustExtractTriggerObj(ctx)
	cf := obj.Get("cf")
	if cf.IsUndefined() {
		return nil, errors.New("runtime is not cloudflare")
	}

	return &IncomingProperties{
		Longitude:                jsutil.MaybeString(cf.Get("longitude")),
		Latitude:                 jsutil.MaybeString(cf.Get("latitude")),
		TLSCipher:                jsutil.MaybeString(cf.Get("tlsCipher")),
		Continent:                jsutil.MaybeString(cf.Get("continent")),
		Asn:                      jsutil.MaybeInt(cf.Get("asn")),
		ClientAcceptEncoding:     jsutil.MaybeString(cf.Get("clientAcceptEncoding")),
		Country:                  jsutil.MaybeString(cf.Get("country")),
		TLSClientAuth:            NewIncomingTLSClientAuth(cf.Get("tlsClientAuth")),
		TLSExportedAuthenticator: NewIncomingTLSExportedAuthenticator(cf.Get("tlsExportedAuthenticator")),
		TLSVersion:               cf.Get("tlsVersion").String(),
		Colo:                     cf.Get("colo").String(),
		Timezone:                 cf.Get("timezone").String(),
		City:                     jsutil.MaybeString(cf.Get("city")),
		VerifiedBotCategory:      jsutil.MaybeString(cf.Get("verifiedBotCategory")),
		RequestPriority:          jsutil.MaybeString(cf.Get("requestPriority")),
		HttpProtocol:             cf.Get("httpProtocol").String(),
		Region:                   jsutil.MaybeString(cf.Get("region")),
		RegionCode:               jsutil.MaybeString(cf.Get("regionCode")),
		AsOrganization:           cf.Get("asOrganization").String(),
		PostalCode:               jsutil.MaybeString(cf.Get("postalCode")),
		BotManagement:            NewIncomingBotManagement(cf.Get("botManagement")),
	}, nil
}
