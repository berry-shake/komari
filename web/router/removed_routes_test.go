package router_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/web/router"
	"github.com/stretchr/testify/require"
)

func TestRemovedMJPEGAndUnknownAPIsReturnNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	router.Register(r)

	for _, path := range []string{
		"/api/mjpeg_live",
		"/api/mjpeg_live?lang=zh&tz_offset=480",
		"/api/mjpeg_live?temp_key=unused",
		"/api/mjpeg_live/",
		"/api/unknown-endpoint",
		"/api",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
			t.Run(method+" "+path, func(t *testing.T) {
				// Bound the request if a streaming handler is accidentally restored.
				ctx, cancel := context.WithTimeout(t.Context(), time.Second)
				defer cancel()
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequestWithContext(ctx, method, path, nil))
				require.Equal(t, http.StatusNotFound, w.Code)
				require.NotContains(t, w.Header().Get("Content-Type"), "multipart/")
				require.NotContains(t, w.Header().Get("Content-Type"), "text/html")
				require.Empty(t, w.Header().Values("Set-Cookie"))
			})
		}
	}

	for _, path := range []string{"/ping", "/api/version"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, path)
	}
}
