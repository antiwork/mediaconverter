package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func useTestS3(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	original := awsClient
	awsClient = s3.New(s3.Options{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		UsePathStyle: true,
		Credentials:  aws.AnonymousCredentials{},
	})
	t.Cleanup(func() { awsClient = original })
}

func writeHLSFiles(t *testing.T, count int) string {
	t.Helper()
	dir := t.TempDir()
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("index_480p_%05d.ts", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte("segment "+name), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestUploadToS3UploadsEveryFile(t *testing.T) {
	var mu sync.Mutex
	uploaded := map[string]string{}
	useTestS3(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		uploaded[r.URL.Path] = string(body)
		mu.Unlock()
	})

	fileCount := maxConcurrentUploads*2 + 1
	if err := uploadToS3(writeHLSFiles(t, fileCount), "s3://gumroad/attachments/abc/video/hls/"); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if len(uploaded) != fileCount {
		t.Fatalf("got %d uploads, want %d", len(uploaded), fileCount)
	}
	if got := uploaded["/gumroad/attachments/abc/video/hls/index_480p_00007.ts"]; got != "segment index_480p_00007.ts" {
		t.Fatalf("got body %q", got)
	}
}

func TestUploadToS3ReturnsAnUploadError(t *testing.T) {
	useTestS3(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "_00003.ts") {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>Access Denied</Message></Error>`)
		}
	})

	err := uploadToS3(writeHLSFiles(t, 10), "s3://gumroad/attachments/abc/video/hls/")
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("got %v, want an AccessDenied error", err)
	}
}
