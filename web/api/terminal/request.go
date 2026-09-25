package terminal

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/utils"
	agent_runtime "github.com/komari-monitor/komari/web/agent"
	"github.com/komari-monitor/komari/web/api"
)

func RequestTerminal(c *gin.Context) {
	uuid := c.Param("uuid")
	user_uuid := c.GetString("uuid")
	_, err := clients.GetClientByUUID(uuid)
	if err != nil {
		c.JSON(400, gin.H{
			"status":  "error",
			"message": "Client not found",
		})
		return
	}
	// 建立ws
	if !api.IsWebSocketUpgrade(c) {
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": "Require WebSocket upgrade"})
		return
	}
	conn, err := api.UpgradeWebSocket(c)
	if err != nil {
		return
	}
	// 新建一个终端连接
	id := utils.GenerateRandomString(32)
	session := &TerminalSession{
		UserUUID:    user_uuid,
		UUID:        uuid,
		Browser:     conn,
		Agent:       nil,
		RequesterIp: c.ClientIP(),
	}

	TerminalSessionsMutex.Lock()
	TerminalSessions[id] = session
	TerminalSessionsMutex.Unlock()
	conn.SetReadLimit(1 << 20)
	control := agent_runtime.GetConnectedClients()[uuid]
	if control == nil {
		session.writeBrowser(1, []byte("Client offline!\n被控端离线!\n"))
		session.close(id)
		return
	}
	session.writeBrowser(1, []byte("等待被控端连接 waiting for agent...\n"))
	if err := control.WriteJSON(gin.H{"message": "terminal", "request_id": id}); err != nil {
		session.close(id)
		return
	}
	time.AfterFunc(30*time.Second, func() {
		session.mu.Lock()
		pending := session.Agent == nil && !session.closed
		if pending {
			session.closed = true
		}
		session.mu.Unlock()
		if pending {
			session.writeBrowser(1, []byte("被控端连接超时 timeout\n"))
			session.close(id)
		}
	})
	//auditlog.Log(c.ClientIP(), user_uuid, "request, terminal id:"+id+",client:"+session.UUID, "terminal")
}
