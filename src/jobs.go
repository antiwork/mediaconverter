package main

import (
	"bytes"
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
	"strconv"
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

var MaxSimultaneousJobs int

var callbackClient = &http.Client{Timeout: 30 * time.Second}

var JobQueue chan Job
var RunningJobs = map[string]Job{}
var JobMutex sync.Mutex

func init() {
	if envMaxSimultaneousJobs := os.Getenv("MAX_SIMULTANEOUS_JOBS"); envMaxSimultaneousJobs != "" {
		var err error
		MaxSimultaneousJobs, err = strconv.Atoi(envMaxSimultaneousJobs)
		if err != nil {
			log.Fatalf("Error: converting MAX_SIMULTANEOUS_JOBS to int: %v", err)
		}
		JobQueue = make(chan Job, MaxSimultaneousJobs)
	} else {
		log.Fatalf("Error: MAX_SIMULTANEOUS_JOBS is not set")
	}

	if os.Getenv("WEBHOOK_SECRET") == "" {
		log.Fatalf("Error: WEBHOOK_SECRET is not set")
	}
}

func jobWorker() {
	for job := range JobQueue {
		processJob(job)
	}
}

func processJob(job Job) {
	log.Printf("[jid: %s] Processing job (%s)\n", job.JobID, job.Request.S3VideoURI)

	updateJobStatus(job, "processing")
	videoPath, HLSDir := setupPaths(job.JobID)

	if err := createTempDirectory(videoPath); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := downloadVideo(job, videoPath); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := runConversionScript(job, videoPath, HLSDir); err != nil {
		handleJobFailure(job, err.Error())
		return
	}

	if err := uploadHLSFiles(job, HLSDir); err != nil {
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

func downloadVideo(job Job, videoPath string) error {
	log.Printf("[jid: %s] Downloading video\n", job.JobID)

	if job.Request.VideoURL != "" {
		return downloadFromURL(job.Request.VideoURL, videoPath)
	}
	return downloadFromS3(job.Request.S3VideoURI, videoPath)
}

func runConversionScript(job Job, inputPath, outputDir string) error {
	log.Printf("[jid: %s] Running conversion script...\n", job.JobID)

	cmd := exec.Command("./ffmpeg/convert-video.sh")
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

func uploadHLSFiles(job Job, localDir string) error {
	log.Printf("[jid: %s] Uploading HLS files\n", job.JobID)

	if err := uploadToS3(localDir, job.Request.S3HLSDirURI); err != nil {
		return fmt.Errorf("failed to upload HLS files: %v", err)
	}
	return nil
}

func finishJobSuccessfully(job Job) {
	finishJob(job, "success", "")
}

func handleJobFailure(job Job, errorMsg string) {
	log.Printf("[jid: %s] Job failed. Error: %s\n", job.JobID, errorMsg)

	finishJob(job, "failure", errorMsg)
}

// Callback retries can sleep for over 20 minutes, so they don't hold the worker or the job's files.
func finishJob(job Job, status string, message string) {
	cleanUpJob(job)
	go postToCallback(job, status, message)
}

func postToCallback(job Job, status string, message string) {
	log.Printf("[jid: %s] Posting callback...\n", job.JobID)

	var payload = map[string]interface{}{}
	payload["id"] = job.Request.ID
	payload["job_id"] = job.JobID
	payload["status"] = status
	if status != "success" {
		payload["message"] = message
	}

	jsonPayload, _ := json.Marshal(payload)

	secret := []byte(os.Getenv("WEBHOOK_SECRET"))
	h := hmac.New(sha256.New, secret)
	h.Write(jsonPayload)
	signature := hex.EncodeToString(h.Sum(nil))

	retryIntervals := []time.Duration{time.Minute * 1, time.Minute * 5, time.Minute * 15, time.Minute * 30}

	for i, interval := range retryIntervals {
		err := sendCallback(job.Request.CallbackURL, jsonPayload, signature)
		if err == nil {
			log.Printf("[jid: %s] Successfully posted callback\n", job.JobID)
			return
		}

		if i == len(retryIntervals)-1 {
			log.Printf("[jid: %s] Failed to post callback after all retries: %v\n", job.JobID, err)
			return
		}

		log.Printf("[jid: %s] Failed to post callback (attempt %d): %v. Retrying in %v...\n", job.JobID, i+1, err, interval)
		time.Sleep(interval)
	}
}

func sendCallback(callbackURL string, payload []byte, signature string) error {
	req, err := http.NewRequest("POST", callbackURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Grmc-Signature", fmt.Sprintf("t=%d,v0=%s", time.Now().UnixMilli(), signature))

	resp, err := callbackClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected response: %s", resp.Status)
	}
	return nil
}

func cleanUpJob(job Job) {
	os.RemoveAll("/tmp/" + job.JobID)
	JobMutex.Lock()
	delete(RunningJobs, job.JobID)
	JobMutex.Unlock()
}
