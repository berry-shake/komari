package github

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStateExpiresAndCanOnlyBeConsumedOnce(t *testing.T) {
	g := &Github{}
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
	_, state := g.GetAuthorizationURL("")
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g.consumeState(state) {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 1 {
		t.Fatalf("accepted %d times", accepted.Load())
	}
	g.states["expired"] = time.Now().Add(-time.Second)
	if g.consumeState("expired") {
		t.Fatal("expired state accepted")
	}
	if g.consumeState("") {
		t.Fatal("empty state accepted")
	}
}

func TestStateCacheIsBounded(t *testing.T) {
	g := &Github{}
	g.Init()
	for i := range 1024 {
		g.states[fmt.Sprint(i)] = time.Now().Add(time.Minute)
	}
	if u, s := g.GetAuthorizationURL(""); u != "" || s != "" {
		t.Fatal("full cache accepted another login")
	}
	g.states["0"] = time.Now().Add(-time.Second)
	if u, s := g.GetAuthorizationURL(""); u == "" || s == "" {
		t.Fatal("expired states did not free capacity")
	}
	if len(g.states) > 1024 {
		t.Fatal("cache exceeded bound")
	}
}
