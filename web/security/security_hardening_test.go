package security

import (
	"context"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPublicDownloadsRejectInternalDialAndRedirect(t *testing.T) {
	for _, value := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "0.0.0.0", "::1", "::ffff:127.0.0.1", "100.100.100.200", "224.0.0.1", "fe80::1"} {
		if publicIP(net.ParseIP(value)) {
			t.Errorf("accepted %s", value)
		}
	}
	if !publicIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IP rejected")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if conn, err := dialPublic(ctx, "tcp", "127.0.0.1:80"); err == nil {
		conn.Close()
		t.Fatal("internal dial accepted")
	}
	old, _ := http.NewRequest("GET", "https://example.org/archive", nil)
	target, _ := http.NewRequest("GET", "http://example.org/archive", nil)
	if PublicHTTPClient().CheckRedirect(target, []*http.Request{old}) == nil {
		t.Fatal("HTTPS downgrade accepted")
	}
}

func TestOneTimeStatesConcurrentReplayAndCapacity(t *testing.T) {
	var s StateStore
	if !s.Put("one", "binding", time.Minute) {
		t.Fatal("put")
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			v, ok := s.Take("one")
			if ok && v == "binding" {
				wins.Add(1)
			}
		})
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("replayed %d times", wins.Load())
	}
	s.Put("expired", "", -time.Second)
	if _, ok := s.Take("expired"); ok {
		t.Fatal("expired accepted")
	}
	for i := range 1024 {
		if !s.Put(string(rune(i+1)), "", time.Minute) {
			t.Fatal("early limit")
		}
	}
	if s.Put("overflow", "", time.Minute) {
		t.Fatal("unbounded state store")
	}
}

func TestHeadersProtectUnauthenticatedResponses(t *testing.T) {
	r := gin.New()
	r.Use(Headers())
	r.GET("/api/private", func(c *gin.Context) { c.AbortWithStatus(401) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/private", nil))
	for _, key := range []string{"Content-Security-Policy", "Referrer-Policy", "X-Frame-Options"} {
		if w.Header().Get(key) == "" {
			t.Fatal(key)
		}
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("401 cacheable")
	}
}
