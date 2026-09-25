package terminal

import (
	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/utils"
	"github.com/komari-monitor/komari/web/security"
	"net/http"
	"time"
)

var tickets security.StateStore

func CreateTicket(c *gin.Context) {
	ticket := utils.GenerateRandomString(48)
	binding := c.GetString("uuid") + "\x00" + c.Param("uuid")
	if !tickets.Put(ticket, binding, 30*time.Second) {
		c.JSON(429, gin.H{"error": "Too many terminal requests"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ticket": ticket})
}

func RequireTicket() gin.HandlerFunc {
	return func(c *gin.Context) {
		binding, ok := tickets.Take(c.Query("ticket"))
		if !ok || binding != c.GetString("uuid")+"\x00"+c.Param("uuid") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired terminal ticket"})
			return
		}
		c.Next()
	}
}
