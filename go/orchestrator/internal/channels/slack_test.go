package channels

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

const testSigningSecret = "test-signing-secret-abc123"

func makeSlackHeaders(t *testing.T, body []byte, secret string, ts int64) http.Header {
	t.Helper()
	tsStr := strconv.FormatInt(ts, 10)
	baseString := fmt.Sprintf("v0:%s:%s", tsStr, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(baseString))
	sig := "v0=" + hex.EncodeToString(mac.Sum(nil))

	h := http.Header{}
	h.Set(slackTimestampHeader, tsStr)
	h.Set(slackSignatureHeader, sig)
	return h
}

func TestVerifySlackSignature_Valid(t *testing.T) {
	body := []byte(`{"type":"event_callback","event":{"type":"message","text":"hello"}}`)
	ts := time.Now().Unix()
	headers := makeSlackHeaders(t, body, testSigningSecret, ts)

	if err := verifySlackSignature(headers, body, testSigningSecret); err != nil {
		t.Fatalf("expected valid signature, got error: %v", err)
	}
}

func TestVerifySlackSignature_InvalidSignature(t *testing.T) {
	body := []byte(`{"type":"event_callback","event":{"type":"message","text":"hello"}}`)
	ts := time.Now().Unix()
	headers := makeSlackHeaders(t, body, testSigningSecret, ts)

	// Tamper with the signature.
	headers.Set(slackSignatureHeader, "v0=0000000000000000000000000000000000000000000000000000000000000000")

	err := verifySlackSignature(headers, body, testSigningSecret)
	if err == nil {
		t.Fatal("expected error for invalid signature, got nil")
	}
}

func TestVerifySlackSignature_OldTimestamp(t *testing.T) {
	body := []byte(`{"type":"event_callback"}`)
	ts := time.Now().Add(-10 * time.Minute).Unix()
	headers := makeSlackHeaders(t, body, testSigningSecret, ts)

	err := verifySlackSignature(headers, body, testSigningSecret)
	if err == nil {
		t.Fatal("expected error for old timestamp, got nil")
	}
}

func TestVerifySlackSignature_MissingHeaders(t *testing.T) {
	body := []byte(`{}`)

	// No headers at all.
	err := verifySlackSignature(http.Header{}, body, testSigningSecret)
	if err == nil {
		t.Fatal("expected error for missing timestamp header, got nil")
	}

	// Timestamp present but no signature.
	h := http.Header{}
	h.Set(slackTimestampHeader, strconv.FormatInt(time.Now().Unix(), 10))
	err = verifySlackSignature(h, body, testSigningSecret)
	if err == nil {
		t.Fatal("expected error for missing signature header, got nil")
	}
}
