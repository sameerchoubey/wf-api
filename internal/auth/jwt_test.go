package auth

import (
	"testing"
	"time"
)

func TestSignAndParse(t *testing.T) {
	const secret = "test-secret"
	tok, err := SignToken("user-123", secret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	id, err := ParseUserID(tok, secret)
	if err != nil {
		t.Fatal(err)
	}
	if id != "user-123" {
		t.Fatalf("got %q", id)
	}
}
