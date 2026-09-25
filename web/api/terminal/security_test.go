package terminal

import (
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"net/http/httptest"
	"testing"
	"time"
)

func TestForeignAgentCannotAttachOrDestroyTerminal(t *testing.T) {
	s := &TerminalSession{UUID: "owner", Browser: &websocket.Conn{}}
	TerminalSessionsMutex.Lock()
	TerminalSessions["secret"] = s
	TerminalSessionsMutex.Unlock()
	t.Cleanup(func() {
		TerminalSessionsMutex.Lock()
		delete(TerminalSessions, "secret")
		TerminalSessionsMutex.Unlock()
	})
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("client_uuid", "foreign") })
	r.GET("/terminal", EstablishConnection)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/terminal?id=secret", nil))
	if w.Code != 403 || lookupSession("secret") != s || s.Agent != nil {
		t.Fatal("foreign agent altered terminal")
	}
}
func TestTicketBoundToUserNodeAndConsumedOnce(t *testing.T) {
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("uuid", c.GetHeader("user")) })
	r.GET("/:uuid", RequireTicket(), func(c *gin.Context) { c.Status(204) })
	run := func(user, node, key string) int {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/"+node+"?ticket="+key, nil)
		req.Header.Set("user", user)
		r.ServeHTTP(w, req)
		return w.Code
	}
	tickets.Put("ok", "admin\x00node", time.Minute)
	if run("admin", "node", "ok") != 204 || run("admin", "node", "ok") != 401 {
		t.Fatal("replay")
	}
	tickets.Put("wrong", "admin\x00node", time.Minute)
	if run("other", "node", "wrong") != 401 {
		t.Fatal("cross-user")
	}
	tickets.Put("wrong-node", "admin\x00node", time.Minute)
	if run("admin", "other", "wrong-node") != 401 {
		t.Fatal("cross-node")
	}
}
