package utils

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestSchemeOnlyTrustsConfiguredProxy(t *testing.T) {
	t.Setenv("KOMARI_TRUSTED_PROXIES", "127.0.0.1")
	for _, tc := range []struct{ peer, scheme string }{{"8.8.8.8:1234", "http"}, {"127.0.0.1:1234", "https"}} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/", nil)
		c.Request.RemoteAddr = tc.peer
		c.Request.Header.Set("X-Forwarded-Proto", "https")
		if GetScheme(c) != tc.scheme {
			t.Fatal(tc.peer)
		}
	}
}
