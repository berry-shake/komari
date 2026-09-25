package client

import (
	"bytes"
	"compress/gzip"
	"net/http/httptest"
	"testing"

	"github.com/komari-monitor/komari/web/security"
)

func TestCompressedBodyLimit(t *testing.T) {
	for _, size := range []int{128, int(security.MaxMessageBytes) + 1} {
		var data bytes.Buffer
		zw := gzip.NewWriter(&data)
		if _, err := zw.Write(bytes.Repeat([]byte("x"), size)); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/clients/v2/rpc", &data)
		r.Header.Set("Content-Encoding", "gzip")
		got, err := readMaybeCompressedBody(r)
		if size > int(security.MaxMessageBytes) {
			if err == nil {
				t.Fatal("accepted compressed body exceeding decoded limit")
			}
		} else if err != nil || len(got) != size {
			t.Fatalf("size=%d err=%v", len(got), err)
		}
	}
}
