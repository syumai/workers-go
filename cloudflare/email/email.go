package email

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"syscall/js"

	"github.com/syumai/workers"
	"github.com/syumai/workers/internal/jsmail"
	"github.com/syumai/workers/internal/jsutil"
	"github.com/syumai/workers/internal/runtimecontext"
)

var (
	emailHandler Handler
	doneCh       = make(chan struct{})
)

func init() {
	emailHandlerFunc := js.FuncOf(func(this js.Value, args []js.Value) any {
		message := args[0]
		var cb js.Func
		cb = js.FuncOf(func(_ js.Value, pArgs []js.Value) any {
			defer cb.Release()
			resolve := pArgs[0]
			reject := pArgs[1]
			go func() {
				if len(args) > 1 {
					reject.Invoke(jsutil.Errorf("too many args given to handleEmail: %d", len(args)))
					return
				}
				err := invokeEmailHandler(message)
				if err != nil {
					reject.Invoke(jsutil.Error(err.Error()))
					return
				}
				resolve.Invoke(js.Undefined())
			}()
			return js.Undefined()
		})
		return jsutil.NewPromise(cb)
	})
	jsutil.Binding.Set("handleEmail", emailHandlerFunc)
}

// Type definitions

type Handler func(m ForwardableEmailMessage) error

// SendableEmailMessage represents an email that can be sent outbound
type SendableEmailMessage interface {
	From() string
	To() string
	Raw() io.ReadCloser
}

// ForwardableEmailMessage is an inbound email that can be forwarded
type ForwardableEmailMessage interface {
	SendableEmailMessage
	Headers() mail.Header
	Forward(rcptTo string, headers mail.Header) error
	Reply(SendableEmailMessage) error
	SetReject(reason string) error
}

// forwardableEmailMessage represents an incoming email message
type forwardableEmailMessage struct {
	obj     js.Value
	from    string
	to      string
	raw     js.Value
	rawSize int
}

// EmailMessage is an outbound email that can be sent
type EmailMessage struct {
	from string
	to   string
	raw  io.ReadCloser
}

// EmailClient is used to send outbound emails
type EmailClient struct {
	bind js.Value
}

// Constructor functions

// NewEmailMessage creates a new outbound email message
func NewEmailMessage(from string, to string, raw io.ReadCloser) *EmailMessage {
	return &EmailMessage{
		from: from,
		to:   to,
		raw:  raw,
	}
}

// NewClient creates a new EmailClient with the given binding
func NewClient(bind js.Value) *EmailClient {
	return &EmailClient{
		bind: bind,
	}
}

// newForwardableEmailMessage creates a forwardableEmailMessage from the given context
func newForwardableEmailMessage(ctx context.Context) (*forwardableEmailMessage, error) {
	obj := runtimecontext.MustExtractTriggerObj(ctx)
	if obj.IsUndefined() {
		return nil, errors.New("email event is null")
	}

	return &forwardableEmailMessage{
		obj:     obj,
		from:    obj.Get("from").String(),
		to:      obj.Get("to").String(),
		raw:     obj.Get("raw"),
		rawSize: obj.Get("rawSize").Int(),
	}, nil
}

// forwardableEmailMessage methods

func (f *forwardableEmailMessage) From() string {
	return f.from
}

func (f *forwardableEmailMessage) To() string {
	return f.to
}

func (f *forwardableEmailMessage) Raw() io.ReadCloser {
	return jsutil.ConvertReadableStreamToReadCloser(f.raw)
}

func (f *forwardableEmailMessage) Headers() mail.Header {
	return jsmail.ToHeader(f.obj.Get("headers"))
}

func (f *forwardableEmailMessage) Forward(rcpTo string, headers mail.Header) error {
	var jsHeaders js.Value

	if headers != nil {
		jsHeaders = jsmail.ToJSHeader(headers)
	}

	prom := f.obj.Call("forward", rcpTo, jsHeaders)
	_, err := jsutil.AwaitPromise(prom)
	return err
}

func (f *forwardableEmailMessage) Reply(message SendableEmailMessage) error {
	msg := SendableEmailMessageToJSEmailMessage(message)
	prom := f.obj.Call("reply", msg)
	_, err := jsutil.AwaitPromise(prom)
	return err
}

func (f *forwardableEmailMessage) SetReject(reason string) error {
	prom := f.obj.Call("setReject", reason)
	_, err := jsutil.AwaitPromise(prom)
	return err
}

// EmailMessage methods

func (e *EmailMessage) From() string {
	return e.from
}

func (e *EmailMessage) To() string {
	return e.to
}

func (e *EmailMessage) Raw() io.ReadCloser {
	return e.raw
}

// EmailClient methods

func (c *EmailClient) Send(m SendableEmailMessage) error {
	if c.bind.IsUndefined() || c.bind.Get("send").IsUndefined() {
		return errors.New("provided email binding not found")
	}
	emailMsg := SendableEmailMessageToJSEmailMessage(m)
	// Call .send on the message
	_, err := jsutil.AwaitPromise(c.bind.Call("send", emailMsg))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

// Public API functions

// Handle registers the email handler and blocks until the worker terminates
func Handle(handler Handler) {
	HandleNonBlock(handler)
	workers.Ready()
	<-Done()
}

// HandleNonBlock registers the email handler without blocking
func HandleNonBlock(handler Handler) {
	emailHandler = handler
}

// Done returns a channel that blocks indefinitely, preventing the worker from terminating
// Just like the cron package, doneCh is never actually closed,
// it's used for blocking/waiting so that worker does not terminate
func Done() <-chan struct{} {
	return doneCh
}

// Internal/helper functions

// invokeEmailHandler is called by the JavaScript runtime when an email is received
func invokeEmailHandler(eventObj js.Value) error {
	ctx := runtimecontext.New(context.Background(), eventObj)
	message, err := newForwardableEmailMessage(ctx)
	if err != nil {
		return err
	}
	return emailHandler(message)
}

// SendableEmailMessageToJSEmailMessage converts a SendableEmailMessage to a JavaScript EmailMessage
func SendableEmailMessageToJSEmailMessage(message SendableEmailMessage) js.Value {
	runtimeCtx := jsutil.RuntimeContext
	emailMessageCtor := runtimeCtx.Get("EmailMessage")
	rawReadableStream := jsutil.ConvertReaderToReadableStream(io.NopCloser(message.Raw()))
	return emailMessageCtor.New(message.From(), message.To(), rawReadableStream)
}
