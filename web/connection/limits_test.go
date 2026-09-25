package connection

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/komari-monitor/komari/web/security"
)

func TestCompressedWebSocketReadIsBounded(t *testing.T) {
	for _, size := range []int{128, int(security.MaxMessageBytes) + 1} {
		result := make(chan error, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := websocket.Upgrader{EnableCompression: true}
			c, err := u.Upgrade(w, r, nil)
			if err != nil {
				result <- err
				return
			}
			defer c.Close()
			c.SetReadLimit(security.MaxMessageBytes)
			c.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, _, err = NewSafeConn(c).ReadMessage()
			result <- err
		}))
		d := websocket.Dialer{EnableCompression: true}
		c, _, err := d.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
		if err != nil {
			srv.Close()
			t.Fatal(err)
		}
		if err := c.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), size)); err != nil {
			t.Error(err)
		}
		err = <-result
		c.Close()
		srv.Close()
		if size > int(security.MaxMessageBytes) && err == nil {
			t.Fatal("decoded message limit not enforced")
		}
		if size < int(security.MaxMessageBytes) && err != nil {
			t.Fatal(err)
		}
	}
}
