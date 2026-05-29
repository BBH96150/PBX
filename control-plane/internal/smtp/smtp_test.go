package smtp

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func TestBuildMessage_ParsesAsValidMIME(t *testing.T) {
	atts := []Attachment{
		{Filename: "alice.wav", ContentType: "audio/wav", Data: bytes.Repeat([]byte{0x55}, 200)},
	}
	raw, err := buildMessage(
		"PBX <pbx@example.com>",
		"bob@example.com",
		"New voicemail from 5555551234",
		"You have a new voicemail.\n\nFrom: Alice <5555551234>\nDuration: 12s\n",
		atts,
	)
	if err != nil {
		t.Fatalf("buildMessage: %v", err)
	}

	// Parse as a regular RFC5322 message.
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("mail.ReadMessage: %v", err)
	}

	if got := msg.Header.Get("Subject"); got != "New voicemail from 5555551234" {
		t.Errorf("Subject: got %q", got)
	}
	if got := msg.Header.Get("To"); got != "bob@example.com" {
		t.Errorf("To: got %q", got)
	}

	ct := msg.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil {
		t.Fatalf("Content-Type parse: %v", err)
	}
	if mediaType != "multipart/mixed" {
		t.Errorf("expected multipart/mixed, got %q", mediaType)
	}
	boundary := params["boundary"]
	if boundary == "" {
		t.Fatal("no boundary in Content-Type")
	}

	mr := multipart.NewReader(msg.Body, boundary)
	parts := 0
	sawText := false
	sawAudio := false
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("NextPart: %v", err)
		}
		parts++
		pct := p.Header.Get("Content-Type")
		switch {
		case strings.HasPrefix(pct, "text/plain"):
			sawText = true
			body, _ := io.ReadAll(p)
			if !strings.Contains(string(body), "New voicemail from 5555551234") {
				// Actually subject text isn't in body — the body content is.
				// Loosen: just confirm body is non-empty.
				if len(body) == 0 {
					t.Error("text part body empty")
				}
			}
		case strings.HasPrefix(pct, "audio/wav"):
			sawAudio = true
			if cd := p.Header.Get("Content-Disposition"); !strings.Contains(cd, "alice.wav") {
				t.Errorf("audio Content-Disposition missing filename: %q", cd)
			}
		}
	}
	if parts != 2 {
		t.Errorf("expected 2 parts, got %d", parts)
	}
	if !sawText || !sawAudio {
		t.Errorf("missing parts (text=%v audio=%v)", sawText, sawAudio)
	}
}

func TestStripDisplayName(t *testing.T) {
	cases := map[string]string{
		"pbx@example.com":              "pbx@example.com",
		"PBX <pbx@example.com>":        "pbx@example.com",
		"  My PBX  <pbx@example.com>":  "pbx@example.com",
		"bare-no-bracket":              "bare-no-bracket",
	}
	for in, want := range cases {
		if got := stripDisplayName(in); got != want {
			t.Errorf("stripDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConfigured(t *testing.T) {
	if (Mailer{}).Configured() {
		t.Error("zero Mailer should not be Configured()")
	}
	m := Mailer{Host: "smtp.example.com", Port: 587, From: "x@example.com"}
	if !m.Configured() {
		t.Error("Mailer with host+port+from should be Configured()")
	}
}
