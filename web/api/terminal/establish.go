package terminal

import (
	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/web/api"
	"net/http"
)

func EstablishConnection(c *gin.Context) {
	id := c.Query("id")
	session := lookupSession(id)
	if session == nil || session.Browser == nil {
		c.JSON(404, gin.H{"error": "Session not found"})
		return
	}
	if c.GetString("client_uuid") == "" || c.GetString("client_uuid") != session.UUID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Terminal identity mismatch"})
		return
	}
	if !api.IsWebSocketUpgrade(c) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Require WebSocket upgrade"})
		return
	}
	session.mu.Lock()
	if session.closed || session.connecting || session.Agent != nil {
		session.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "Session already connected or closed"})
		return
	}
	session.connecting = true
	session.mu.Unlock()
	conn, err := api.UpgradeWebSocket(c)
	session.mu.Lock()
	session.connecting = false
	if err != nil {
		session.mu.Unlock()
		return
	}
	if session.closed {
		session.mu.Unlock()
		conn.Close()
		return
	}
	conn.SetReadLimit(1 << 20)
	session.Agent = conn
	session.mu.Unlock()
	go ForwardTerminal(id)
}
