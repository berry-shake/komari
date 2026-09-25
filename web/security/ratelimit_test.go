package security

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAttemptsExpireAndStorageIsBounded(t *testing.T) {
	l := NewAttemptLimiter(2, time.Minute)
	now := time.Now()
	if !l.allowAt("user", now) || !l.allowAt("user", now) || l.allowAt("user", now) {
		t.Fatal("attempt limit failed")
	}
	if !l.allowAt("user", now.Add(time.Minute)) {
		t.Fatal("attempt window did not expire")
	}
	l = NewAttemptLimiter(1, time.Minute)
	for i := range 4096 {
		if !l.allowAt(fmt.Sprint(i), now) {
			t.Fatal("unexpected early capacity limit")
		}
	}
	if l.allowAt("overflow", now) || len(l.entries) > 4096 {
		t.Fatal("unbounded identity map")
	}
	if !l.allowAt("new", now.Add(time.Minute)) {
		t.Fatal("expired identities not removed")
	}
}

func TestPeerLimitDoesNotTrustForwardedHeaders(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/login", nil)
	r.RemoteAddr = "192.0.2.1:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.2")
	if got := PeerKey(r); got != "192.0.2.1" {
		t.Fatalf("peer=%s", got)
	}
}
