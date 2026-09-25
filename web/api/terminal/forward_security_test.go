package terminal

import (
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/komari-monitor/komari/cmd/flags"
	"github.com/komari-monitor/komari/database/dbcore"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTerminalRoundTripEmptyFrameAndCleanup(t *testing.T) {
	flags.DatabaseType = "sqlite"
	flags.DatabaseFile = "file:terminal-security?mode=memory&cache=shared"
	dbcore.GetDBInstance()
	ready := make(chan struct{})
	r := gin.New()
	r.GET("/browser", func(c *gin.Context) {
		conn, err := (&websocket.Upgrader{}).Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			return
		}
		TerminalSessionsMutex.Lock()
		TerminalSessions["roundtrip"] = &TerminalSession{UUID: "node", UserUUID: "admin", Browser: conn}
		TerminalSessionsMutex.Unlock()
		close(ready)
	})
	r.GET("/agent", func(c *gin.Context) { c.Set("client_uuid", c.GetHeader("X-Test-Node")); EstablishConnection(c) })
	srv := httptest.NewServer(r)
	defer srv.Close()
	browser, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/browser", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()
	<-ready
	agent, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/agent?id=roundtrip", map[string][]string{"X-Test-Node": {"node"}, "X-Client-Token": {"test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer agent.Close()
	browser.SetReadDeadline(time.Now().Add(3 * time.Second))
	agent.SetReadDeadline(time.Now().Add(3 * time.Second))
	if err := browser.WriteMessage(websocket.TextMessage, nil); err != nil {
		t.Fatal(err)
	}
	kind, payload, err := agent.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || len(payload) != 0 {
		t.Fatalf("empty frame: %d %v", kind, err)
	}
	if err := agent.WriteMessage(websocket.BinaryMessage, []byte("terminal output")); err != nil {
		t.Fatal(err)
	}
	_, payload, err = browser.ReadMessage()
	if err != nil || string(payload) != "terminal output" {
		t.Fatal("return path failed", err)
	}
	browser.Close()
	deadline := time.Now().Add(3 * time.Second)
	for lookupSession("roundtrip") != nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if lookupSession("roundtrip") != nil {
		t.Fatal("session not removed")
	}
}
