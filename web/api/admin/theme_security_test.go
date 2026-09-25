package admin

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestThemePathsStayInsideRoot(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("data/theme/valid-theme", 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/victim", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.POST("/delete", DeleteTheme)
	r.GET("/set", SetTheme)
	r.POST("/update", UpdateTheme)
	for _, path := range []string{"/delete", "/update"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("POST", path, strings.NewReader(`{"short":"../victim"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/set?theme=../victim", nil))
	if w.Code != 400 {
		t.Fatalf("set returned %d", w.Code)
	}
	if b, err := os.ReadFile("data/victim"); err != nil || string(b) != "keep" {
		t.Fatalf("victim changed: %s %v", b, err)
	}
	for _, short := range []string{"", "..", "/tmp", `..\victim`, "a/b", "default"} {
		if _, err := checkedThemeDir(short, false); err == nil {
			t.Errorf("accepted %q", short)
		}
	}
	if err := os.Symlink("../victim", "data/theme/link"); err == nil {
		if _, err := checkedThemeDir("link", false); err == nil {
			t.Fatal("accepted symlink")
		}
	}
	w = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/delete", strings.NewReader(`{"short":"valid-theme"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("valid deletion failed: %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join("data", "theme", "valid-theme")); !os.IsNotExist(err) {
		t.Fatal("valid theme still exists")
	}
}
