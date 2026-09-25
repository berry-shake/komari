package log

import (
	"net/url"
	"strings"
	"testing"
)

func TestRequestQueriesDoNotLogCredentials(t *testing.T) {
	result := redactQuery("token=node-secret&Authorization=admin-secret&code=oauth-secret&uuid=keep-node-id")
	if strings.Contains(result, "secret") {
		t.Fatal("credential logged")
	}
	values, err := url.ParseQuery(result)
	if err != nil || values.Get("uuid") != "keep-node-id" || values.Get("token") != "[REDACTED]" {
		t.Fatalf("redacted=%q err=%v", result, err)
	}
	if got := redactQuery("token=secret%broken"); strings.Contains(got, "secret") {
		t.Fatal("malformed query leaked")
	}
}
