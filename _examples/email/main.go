package main

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/textproto"
	"os"
	"strings"
	"time"

	"github.com/syumai/workers/cloudflare"
	"github.com/syumai/workers/cloudflare/email"
)

var emailDomain string = cloudflare.Getenv("EMAIL_DOMAIN")
var verifiedDesinationAddress string = cloudflare.Getenv("VERIFIED_DESTINATION_ADDRESS")
var logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))

func HandleEmail(emailMessage email.ForwardableEmailMessage) error {
	var err error

	rawEmail := emailMessage.Raw()
	msg, err := io.ReadAll(emailMessage.Raw())
	if err != nil {
		return err
	}
	defer rawEmail.Close()

	logger.Info("received email",
		"from", emailMessage.Headers().Get("From"),
		"to", emailMessage.Headers().Get("To"),
		"raw", string(msg),
	)

	subject := emailMessage.Headers().Get("Subject")

	switch {
	case strings.Contains(subject, "please reply"):
		err = reply(emailMessage, []byte("I got your message"))
		if err != nil {
			return err
		}
	case strings.Contains(subject, "important"):
		err = emailMessage.Forward(verifiedDesinationAddress, nil)
		if err != nil {
			return err
		}
	default:
		from := fmt.Sprintf("no-reply%s", emailDomain)
		send(from, verifiedDesinationAddress, "Someone sent us an email", []byte("Someone sent us a message!"))
	}

	return nil
}

func buildSimpleEmail(from, to, subject string, headers map[string]string, body []byte) (*bytes.Buffer, error) {
	buf := new(bytes.Buffer)

	// Write headers
	fmt.Fprintf(buf, "From: <%s>\r\n", from)
	fmt.Fprintf(buf, "To: <%s>\r\n", to)
	fmt.Fprintf(buf, "Subject: %s\r\n", subject)
	fmt.Fprintf(buf, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(buf, "Message-ID: <%d@%s>\r\n", time.Now().UnixNano(), emailDomain)

	// Write custom headers
	for k, v := range headers {
		// Headers must start with Capital Letters
		fmt.Fprintf(buf, "%s: %s\r\n", textproto.CanonicalMIMEHeaderKey(k), v)
	}

	// Empty line separates headers from body
	fmt.Fprintf(buf, "\r\n")

	// Write body
	buf.Write(body)

	return buf, nil
}

func reply(msg email.ForwardableEmailMessage, body []byte) error {
	from := msg.To()
	to := msg.From()
	subject := fmt.Sprintf("Re: %s", msg.Headers().Get("Subject"))

	// Build reply headers
	headers := map[string]string{
		"In-Reply-To": msg.Headers().Get("Message-ID"),
		"References":  msg.Headers().Get("Message-ID"),
	}

	buf, err := buildSimpleEmail(from, to, subject, headers, body)
	if err != nil {
		return fmt.Errorf("error building reply: %w", err)
	}

	reply := email.NewEmailMessage(from, to, io.NopCloser(buf))

	err = msg.Reply(reply)
	return err
}

func send(from string, to string, subject string, body []byte) error {
	buf, err := buildSimpleEmail(from, to, subject, nil, body)
	if err != nil {
		return fmt.Errorf("error building email: %w", err)
	}
	mailClient := email.NewClient(cloudflare.GetBinding("EMAIL"))
	err = mailClient.Send(email.NewEmailMessage(from, to, io.NopCloser(buf)))
	if err != nil {
		return fmt.Errorf("error sending email %w", err)
	}
	return nil
}

func main() {
	email.Handle(HandleEmail)
}
