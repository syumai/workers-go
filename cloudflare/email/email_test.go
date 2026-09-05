package email

import (
	"io"
	"strings"
	"syscall/js"
	"testing"
)

// TestNewEmailMessage tests the EmailMessage constructor
func TestNewEmailMessage(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		raw  io.ReadCloser
	}{
		{
			name: "basic email message",
			from: "sender@example.com",
			to:   "recipient@example.com",
			raw:  io.NopCloser(strings.NewReader("test email body")),
		},
		{
			name: "nil reader",
			from: "sender@example.com",
			to:   "recipient@example.com",
			raw:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := NewEmailMessage(tt.from, tt.to, tt.raw)

			if msg.From() != tt.from {
				t.Errorf("From() = %q, want %q", msg.From(), tt.from)
			}

			if msg.To() != tt.to {
				t.Errorf("To() = %q, want %q", msg.To(), tt.to)
			}

			if msg.Raw() != tt.raw {
				t.Errorf("Raw() = %v, want %v", msg.Raw(), tt.raw)
			}
		})
	}
}

// TestEmailClientSend tests the EmailClient Send method
func TestEmailClientSend(t *testing.T) {
	tests := []struct {
		name        string
		binding     js.Value
		sendMethod  js.Value
		expectError bool
		errorMsg    string
	}{
		{
			name:        "undefined binding",
			binding:     js.Undefined(),
			expectError: true,
			errorMsg:    "binding not found",
		},
		{
			name: "binding without send method",
			binding: js.ValueOf(map[string]interface{}{
				"other": "value",
			}),
			expectError: true,
			errorMsg:    "binding not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.binding)
			msg := NewEmailMessage("from@test.com", "to@test.com", io.NopCloser(strings.NewReader("test")))

			err := client.Send(msg)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error containing %q, got nil", tt.errorMsg)
				} else if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("Expected error containing %q, got %q", tt.errorMsg, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// TestForwardableEmailMessageMethods tests the basic methods of ForwardableEmailMessage
func TestForwardableEmailMessageMethods(t *testing.T) {
	// Create a mock js.Value that looks like an email message
	mockJSValue := js.ValueOf(map[string]interface{}{
		"from":    "sender@example.com",
		"to":      "recipient@example.com",
		"raw":     js.Undefined(), // Will be mocked in actual implementation
		"rawSize": 100,
		"headers": js.ValueOf(map[string]interface{}{
			"Subject": "Test Email",
			"From":    "Sender <sender@example.com>",
		}),
	})

	// Create forwardableEmailMessage directly
	f := &forwardableEmailMessage{
		obj:     mockJSValue,
		from:    "sender@example.com",
		to:      "recipient@example.com",
		raw:     js.Undefined(),
		rawSize: 100,
	}

	tests := []struct {
		name     string
		testFunc func() interface{}
		expected interface{}
	}{
		{
			name: "From method",
			testFunc: func() interface{} {
				return f.From()
			},
			expected: "sender@example.com",
		},
		{
			name: "To method",
			testFunc: func() interface{} {
				return f.To()
			},
			expected: "recipient@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.testFunc()
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}
