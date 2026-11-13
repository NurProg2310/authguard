package security

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse 1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$") {
		t.Fatalf("unexpected format %q", h)
	}
	if ok, _ := VerifyPassword("correct horse 1", h); !ok {
		t.Fatal("expected match")
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("expected mismatch")
	}
	if _, err := VerifyPassword("x", "garbage"); err == nil {
		t.Fatal("expected error on malformed hash")
	}
}

func TestTokenTypesAreNotInterchangeable(t *testing.T) {
	m := NewTokenManager([]byte(strings.Repeat("k", 32)), "iss", "aud", time.Minute, time.Minute)
	uid, sid := uuid.New(), uuid.New()

	access, _, _ := m.IssueAccess(uid, sid)
	takeover, _, _ := m.IssueTakeover(uid)

	c, err := m.ParseAccess(access)
	if err != nil || c.SessionID != sid.String() || c.Subject != uid.String() {
		t.Fatalf("access parse: %v %+v", err, c)
	}
	if _, err := m.ParseAccess(takeover); err == nil {
		t.Fatal("takeover token accepted as access token")
	}
	if _, err := m.ParseTakeover(access); err == nil {
		t.Fatal("access token accepted as takeover token")
	}

	other := NewTokenManager([]byte(strings.Repeat("z", 32)), "iss", "aud", time.Minute, time.Minute)
	if _, err := other.ParseAccess(access); err == nil {
		t.Fatal("token verified with wrong key")
	}
}

func TestExpiredToken(t *testing.T) {
	m := NewTokenManager([]byte(strings.Repeat("k", 32)), "iss", "aud", -time.Minute, time.Minute)
	tok, _, _ := m.IssueAccess(uuid.New(), uuid.New())
	if _, err := m.ParseAccess(tok); err != ErrTokenExpired {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}
