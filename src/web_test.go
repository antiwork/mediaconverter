package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func requestWithKey(key string) error {
	r := httptest.NewRequest("GET", "/status", nil)
	r.SetBasicAuth(key, "")
	return AuthenticateRequest(r)
}

func TestAuthenticateRequestAcceptsEveryListedKey(t *testing.T) {
	t.Setenv("API_KEY", "new-key, old-key")

	for _, key := range []string{"new-key", "old-key"} {
		if err := requestWithKey(key); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
	}
}

func TestAuthenticateRequestRejectsOtherKeys(t *testing.T) {
	t.Setenv("API_KEY", "new-key,old-key")

	for _, key := range []string{"other-key", "", "new-key,old-key"} {
		if err := requestWithKey(key); err == nil {
			t.Fatalf("%q was accepted", key)
		}
	}
}

func TestAuthenticateRequestRejectsEverythingWithoutAKey(t *testing.T) {
	t.Setenv("API_KEY", "")

	if err := requestWithKey(""); err == nil {
		t.Fatal("an empty key was accepted")
	}
}

func TestHandleUpFailsWithoutAUsableAPIKey(t *testing.T) {
	for name, value := range map[string]string{"PORT": "80", "MAX_SIMULTANEOUS_JOBS": "1", "AWS_REGION": "us-east-1", "AWS_ACCESS_KEY_ID": "id", "AWS_SECRET_ACCESS_KEY": "secret"} {
		t.Setenv(name, value)
	}

	for apiKey, want := range map[string]int{",": http.StatusInternalServerError, " ": http.StatusInternalServerError, "key": http.StatusOK} {
		t.Setenv("API_KEY", apiKey)
		w := httptest.NewRecorder()
		handleUp(w, httptest.NewRequest("GET", "/up", nil))
		if w.Code != want {
			t.Fatalf("API_KEY=%q: got %d, want %d", apiKey, w.Code, want)
		}
	}
}
