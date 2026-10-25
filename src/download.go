package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"
)

const maxDownloadAttempts = 5

var downloadRetryDelay = 5 * time.Second

var downloadIdleTimeout = time.Minute

var downloadClient = func() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = time.Minute
	return &http.Client{Transport: transport}
}()

// Downloads over HTTP, resuming with a Range request after a dropped connection.
func downloadFromURL(videoURL string, localPath string) error {
	log.Printf("downloading from %s to %s\n", redactQuery(videoURL), localPath)

	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %v", err)
	}
	defer localFile.Close()

	var lastErr error
	for attempt := 1; attempt <= maxDownloadAttempts; attempt++ {
		retryable, err := downloadRemainder(videoURL, localFile)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable || attempt == maxDownloadAttempts {
			break
		}

		log.Printf("download attempt %d failed: %v\n", attempt, err)
		time.Sleep(time.Duration(attempt) * downloadRetryDelay)
	}

	return fmt.Errorf("failed to download video: %v", lastErr)
}

func downloadRemainder(videoURL string, localFile *os.File) (retryable bool, err error) {
	offset, err := localFile.Seek(0, io.SeekEnd)
	if err != nil {
		return false, err
	}

	req, err := http.NewRequest("GET", videoURL, nil)
	if err != nil {
		return false, redactURLError(err)
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}

	resp, err := downloadClient.Do(req)
	if err != nil {
		return true, redactURLError(err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusPartialContent && offset > 0:
	case resp.StatusCode == http.StatusOK:
		if offset > 0 {
			// The server ignored the Range header, so start over.
			if err := localFile.Truncate(0); err != nil {
				return false, err
			}
			if _, err := localFile.Seek(0, io.SeekStart); err != nil {
				return false, err
			}
		}
	default:
		return resp.StatusCode >= 500, fmt.Errorf("unexpected response: %s", resp.Status)
	}

	// A connection that stays open but stops sending would otherwise block the job forever.
	stalled := time.AfterFunc(downloadIdleTimeout, func() { resp.Body.Close() })
	defer stalled.Stop()

	if _, err := io.Copy(localFile, &idleTimeoutReader{resp.Body, stalled}); err != nil {
		return true, err
	}
	return false, nil
}

type idleTimeoutReader struct {
	body  io.Reader
	timer *time.Timer
}

func (r *idleTimeoutReader) Read(p []byte) (int, error) {
	r.timer.Reset(downloadIdleTimeout)
	return r.body.Read(p)
}

// A url.Error's message includes the full URL, signature and all.
func redactURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		urlErr.URL = redactQuery(urlErr.URL)
	}
	return err
}

// The query string carries the URL's signature.
func redactQuery(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "[unparseable URL]"
	}
	parsed.RawQuery = ""
	return parsed.String()
}
