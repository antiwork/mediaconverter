package main

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestSendCallbackSignsTheRequest(t *testing.T) {
	var signature string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signature = r.Header.Get("Grmc-Signature")
	}))
	defer server.Close()

	if err := sendCallback(server.URL, []byte(`{}`), "abc123"); err != nil {
		t.Fatalf("callback failed: %v", err)
	}
	if !regexp.MustCompile(`\At=\d+,v0=abc123\z`).MatchString(signature) {
		t.Fatalf("got signature header %q", signature)
	}
}

func TestSendCallbackReturnsAnErrorWhenTheCallbackFails(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer failing.Close()
	unreachable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable.Close()

	for name, callbackURL := range map[string]string{"non-200 response": failing.URL, "unreachable": unreachable.URL} {
		t.Run(name, func(t *testing.T) {
			if err := sendCallback(callbackURL, []byte(`{}`), "abc123"); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
