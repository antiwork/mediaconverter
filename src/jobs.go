package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"sync"
	"time"
)

type Job struct {
	JobID     string
	Request   ConvertRequest
	Status    string
	ErrorMsg  string
	StartTime time.Time
}

var RunningJobs = map[string]Job{}
var JobMutex sync.Mutex

func jobWorker(ctx context.Context) {
	for job := range JobQueue {
		processJob(ctx, job)
	}
}

func processJob(ctx context.Context, job Job) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()

	log.Printf("[jid: %s] Processing job (%s)\n", job.JobID, job.Request.S3VideoURI)

	updateJobStatus(job, "processing")
	videoPath, HLSDir := setupPaths(job.JobID)

	if err := createTempDirectory(videoPath); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := downloadVideo(ctx, job, videoPath); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := runConversionScript(ctx, job, videoPath, HLSDir); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := uploadHLSFiles(ctx, job, HLSDir); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	finishJobSuccessfully(job)

	log.Printf("[jid: %s] Finished processing successfully\n", job.JobID)
}

func updateJobStatus(job Job, status string) {
	JobMutex.Lock()
	defer JobMutex.Unlock()
	job.Status = status
	RunningJobs[job.JobID] = job
}

func setupPaths(jobID string) (string, string) {
	return fmt.Sprintf("/tmp/%s/in/video", jobID), fmt.Sprintf("/tmp/%s/out", jobID)
}

func downloadVideo(ctx context.Context, job Job, videoPath string) error {
	log.Printf("[jid: %s] Downloading video\n", job.JobID)

	return downloadFromS3(ctx, job.Request.S3VideoURI, videoPath)
}

func runConversionScript(ctx context.Context, job Job, inputPath, outputDir string) error {
	log.Printf("[jid: %s] Running conversion script...\n", job.JobID)

	cmd := exec.CommandContext(ctx, "./ffmpeg/convert-video.sh")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("input_video=%s", inputPath),
		fmt.Sprintf("output_dir=%s", outputDir),
		fmt.Sprintf("hls_480p=%d", boolToInt(slices.Contains(job.Request.Presets, "hls_480p"))),
		fmt.Sprintf("hls_720p=%d", boolToInt(slices.Contains(job.Request.Presets, "hls_720p"))),
		fmt.Sprintf("hls_1080p=%d", boolToInt(slices.Contains(job.Request.Presets, "hls_1080p"))),
	)

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run conversion script: %v", err)
	}
	return nil
}

func uploadHLSFiles(ctx context.Context, job Job, localDir string) error {
	log.Printf("[jid: %s] Uploading HLS files\n", job.JobID)

	if err := uploadToS3(ctx, localDir, job.Request.S3HLSDirURI); err != nil {
		return fmt.Errorf("failed to upload HLS files: %v", err)
	}
	return nil
}

func finishJobSuccessfully(job Job) {
	postToCallback(job, "success", "")
	cleanUpJob(job)
}

func handleJobFailure(job Job, errorMsg string) {
	log.Printf("[jid: %s] Job failed. Error: %s\n", job.JobID, errorMsg)

	postToCallback(job, "failure", errorMsg)
	cleanUpJob(job)
}

type CallbackPayload struct {
	ID      string `json:"id"`
	JobID   string `json:"job_id"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func postToCallback(job Job, status string, message string) {
	log.Printf("[jid: %s] Posting callback...\n", job.JobID)

	payload := CallbackPayload{
		ID:     job.Request.ID,
		JobID:  job.JobID,
		Status: status,
	}
	if message != "success" {
		payload.Message = message
	}

	jsonPayload, _ := json.Marshal(payload)

	secret := []byte(os.Getenv("WEBHOOK_SECRET"))
	h := hmac.New(sha256.New, secret)
	h.Write(jsonPayload)
	signature := hex.EncodeToString(h.Sum(nil))

	retryIntervals := []time.Duration{time.Minute * 1, time.Minute * 5, time.Minute * 15, time.Minute * 30}
	var resp *http.Response
	var err error

	for i, interval := range retryIntervals {

		req, reqErr := http.NewRequest("POST", job.Request.CallbackURL, bytes.NewReader(jsonPayload))
		if reqErr != nil {
			log.Printf("[jid: %s] Failed to create request: %v\n", job.JobID, reqErr)
			continue
		}
		req.Header.Set("Content-Type", "application/json")

		timestamp := time.Now().UnixMilli()
		req.Header.Set("Grmc-Signature", fmt.Sprintf("t=%d,v0=%s", timestamp, signature))

		client := &http.Client{}
		resp, err = client.Do(req)
		if err == nil && resp.StatusCode == 200 {
			log.Printf("[jid: %s] Successfully posted callback\n", job.JobID)
			break
		}

		if i == len(retryIntervals)-1 {
			log.Printf("[jid: %s] Failed to post callback after all retries: %v\n", job.JobID, err)
			break
		}

		log.Printf("[jid: %s] Failed to post callback (attempt %d): %v / %s. Retrying in %v...\n", job.JobID, i+1, err, resp.Status, interval)
		time.Sleep(interval)
	}

	if resp != nil {
		resp.Body.Close()
	}
}

func cleanUpJob(job Job) {
	if err := os.RemoveAll("/tmp/" + job.JobID); err != nil {
		log.Printf("[jid: %s] Failed to cleanup job files: %v", job.JobID, err)
	}

	JobMutex.Lock()
	delete(RunningJobs, job.JobID)
	JobMutex.Unlock()
}
