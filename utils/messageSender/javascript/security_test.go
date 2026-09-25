package javascript

import (
	"github.com/dop251/goja"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimersPromisesAndFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) }))
	defer srv.Close()
	j := &JavaScriptSender{Addition: Addition{Script: `async function sendMessage(){ await new Promise(resolve=>setTimeout(resolve,5)); let r=await fetch("` + srv.URL + `"); return (await r.json()).ok; }`}}
	if err := j.Init(); err != nil {
		t.Fatal(err)
	}
	defer j.Destroy()
	if err := j.SendTextMessage("message", "title"); err != nil {
		t.Fatal(err)
	}
}
func TestTimerQuotaAndExecutionInterrupt(t *testing.T) {
	j := &JavaScriptSender{Addition: Addition{Script: `function sendMessage(){ for(let i=0;i<10000;i++)setTimeout(()=>{},100); return true; }`}}
	if err := j.Init(); err != nil {
		t.Fatal(err)
	}
	if err := j.SendTextMessage("", ""); err == nil {
		t.Fatal("timer quota not enforced")
	}
	j.vm = goja.New()
	stop := j.beginExecution(30 * time.Millisecond)
	started := time.Now()
	if _, err := j.vm.RunString(`while(true){}`); err == nil {
		t.Fatal("infinite script did not stop")
	}
	stop()
	if time.Since(started) > time.Second {
		t.Fatal("deadline not enforced")
	}
	if _, err := j.vm.RunString(`1+1`); err != nil {
		t.Fatal("runtime did not recover")
	}
}
