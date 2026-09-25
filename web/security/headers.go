package security

import (
	"crypto/sha256"
	"crypto/subtle"
	"github.com/gin-gonic/gin"
	"strings"
)

func EqualSecret(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	x, y := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(x[:], y[:]) == 1
}

func Headers() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "frame-ancestors 'self'; object-src 'none'; base-uri 'self'")
		if strings.HasPrefix(c.Request.URL.Path, "/api") || strings.HasPrefix(c.Request.URL.Path, "/admin") || c.Query("temp_key") != "" {
			c.Header("Cache-Control", "no-store")
		}
		c.Next()
	}
}
