package public

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/web/security"
)

func TestLoginAttemptsAreLimitedAcrossSourceAddresses(t *testing.T) {
	oldPeers, oldAccounts := loginPeers, loginAccounts
	defer func() { loginPeers = oldPeers; loginAccounts = oldAccounts }()
	loginPeers = security.NewAttemptLimiter(30, time.Minute)
	loginAccounts = security.NewAttemptLimiter(10, time.Minute)
	r := gin.New()
	r.POST("/login", Login)
	for i := range 11 {
		req := httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"security-missing-user","password":"invalid"}`))
		req.RemoteAddr = fmt.Sprintf("192.0.2.%d:1234", i+1)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 401
		if i == 10 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d: status %d body=%s", i+1, w.Code, w.Body.String())
		}
	}
}
