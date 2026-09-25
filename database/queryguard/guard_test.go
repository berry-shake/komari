package queryguard

import (
	"testing"
	"time"
)

func TestBounds(t *testing.T) {
	now := time.Now()
	if !ValidRange(now.Add(-time.Hour), now) || ValidRange(now, now.Add(-time.Second)) || ValidRange(now.Add(-367*24*time.Hour), now) {
		t.Fatal("range bounds")
	}
	for range 4 {
		if !Acquire() {
			t.Fatal("slot unavailable")
		}
	}
	if Acquire() {
		t.Fatal("unbounded queries")
	}
	for range 4 {
		Release()
	}
}
