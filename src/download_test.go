package main

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

var testVideo = bytes.Repeat([]byte("0123456789"), 10_000)

func init() {
	downloadRetryDelay = 0
	downloadIdleTimeout = 200 * time.Millisecond
}

func writeHalfAndDropConnection(w http.ResponseWriter) {
	w.Header().Set("Content-Length", strconv.Itoa(len(testVideo)))
	w.WriteHeader(http.StatusOK)
	w.Write(testVideo[:len(testVideo)/2])
	w.(http.Flusher).Flush()
	conn, _, _ := w.(http.Hijacker).Hijack()
	conn.Close()
}

func download(t *testing.T, handler http.HandlerFunc) ([]byte, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()

	path := filepath.Join(t.TempDir(), "video")
	err := downloadFromURL(server.URL+"/video.mp4?Signature=secret", path)
	got, _ := os.ReadFile(path)
	return got, err
}

func expectFullVideo(t *testing.T, got []byte, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if !bytes.Equal(got, testVideo) {
		t.Fatalf("got %d bytes, want %d", len(got), len(testVideo))
	}
}

func TestDownloadFromURLResumesAfterDroppedConnection(t *testing.T) {
	var ranges []string
	got, err := download(t, func(w http.ResponseWriter, r *http.Request) {
		ranges = append(ranges, r.Header.Get("Range"))
		if len(ranges) == 1 {
			writeHalfAndDropConnection(w)
			return
		}
		http.ServeContent(w, r, "video.mp4", time.Time{}, bytes.NewReader(testVideo))
	})

	expectFullVideo(t, got, err)
	wantRanges := []string{"", fmt.Sprintf("bytes=%d-", len(testVideo)/2)}
	if fmt.Sprint(ranges) != fmt.Sprint(wantRanges) {
		t.Fatalf("got ranges %q, want %q", ranges, wantRanges)
	}
}

func TestDownloadFromURLRestartsWhenRangeIsIgnored(t *testing.T) {
	requests := 0
	got, err := download(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			writeHalfAndDropConnection(w)
			return
		}
		w.Write(testVideo)
	})

	expectFullVideo(t, got, err)
}

func TestDownloadFromURLResumesAfterTheConnectionStalls(t *testing.T) {
	requests := 0
	got, err := download(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if requests == 1 {
			w.Header().Set("Content-Length", strconv.Itoa(len(testVideo)))
			w.Write(testVideo[:len(testVideo)/2])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "video.mp4", time.Time{}, bytes.NewReader(testVideo))
	})

	expectFullVideo(t, got, err)
}

func TestDownloadFromURLRetriesOnlyServerErrors(t *testing.T) {
	for _, tc := range []struct {
		status       int
		wantRequests int
	}{
		{http.StatusForbidden, 1},
		{http.StatusBadGateway, maxDownloadAttempts},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			requests := 0
			_, err := download(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.WriteHeader(tc.status)
			})

			if err == nil {
				t.Fatal("expected an error")
			}
			if requests != tc.wantRequests {
				t.Fatalf("got %d requests, want %d", requests, tc.wantRequests)
			}
		})
	}
}

func TestDownloadFromURLKeepsTheSignatureOutOfErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	err := downloadFromURL(server.URL+"/video.mp4?Signature=secret", filepath.Join(t.TempDir(), "video"))
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("got %v, want an error without the signature", err)
	}
}

func TestRedactQueryDropsTheSignature(t *testing.T) {
	got := redactQuery("https://d2dw6lv4z9w0e2.cloudfront.net/a/video.mp4?Expires=1&Signature=secret&Key-Pair-Id=K")
	if want := "https://d2dw6lv4z9w0e2.cloudfront.net/a/video.mp4"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
