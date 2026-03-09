package channels

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"testing"
)

func TestVerifyLINESignature_Valid(t *testing.T) {
	secret := "test-channel-secret"
	body := []byte(`{"events":[{"type":"message"}]}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	r, _ := http.NewRequest("POST", "/", nil)
	r.Header.Set("X-Line-Signature", sig)

	err := verifyLINESignature(r, body, secret)
	if err != nil {
		t.Errorf("expected valid signature, got error: %v", err)
	}
}

func TestVerifyLINESignature_Invalid(t *testing.T) {
	r, _ := http.NewRequest("POST", "/", nil)
	r.Header.Set("X-Line-Signature", "badsig")

	err := verifyLINESignature(r, []byte("body"), "secret")
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifyLINESignature_MissingHeader(t *testing.T) {
	r, _ := http.NewRequest("POST", "/", nil)
	err := verifyLINESignature(r, []byte("body"), "secret")
	if err == nil {
		t.Error("expected error for missing header")
	}
}
