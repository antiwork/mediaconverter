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
	"strconv"
	"strings"
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

	return downloadFromS3(job.Request.S3VideoURI, videoPath)
}

func detectVideoCodec(job Job, videoPath string) (string, error) {
	log.Printf("[jid: %s] Detecting video codec...\n", job.JobID)

	cmd := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_name",
		"-of", "default=nw=1:nk=1",
		videoPath)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to detect codec: %v", err)
	}

	codec := strings.TrimSpace(string(output))
	log.Printf("[jid: %s] Detected codec: %s\n", job.JobID, codec)

	return codec, nil
}

func determineScaleFilter(codec string) string {
	if codec == "dxv" {
		return "zscale"
	}
	return "scale"
}

func runConversionScript(job Job, inputPath, outputDir string) error {
	log.Printf("[jid: %s] Running conversion script...\n", job.JobID)

	codec, err := detectVideoCodec(job, inputPath)
	scaleFilter := "scale"
	if err != nil {
		log.Printf("[jid: %s] Warning: codec detection failed (%v), defaulting to scale filter\n", job.JobID, err)
	} else {
		scaleFilter = determineScaleFilter(codec)
		log.Printf("[jid: %s] Using %s filter for codec %s\n", job.JobID, scaleFilter, codec)
	}

	cmd := exec.Command("./ffmpeg/convert-video.sh")
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("input_video=%s", inputPath),
		fmt.Sprintf("output_dir=%s", outputDir),
		fmt.Sprintf("hls_480p=%d", boolToInt(contains(job.Request.Presets, "hls_480p"))),
		fmt.Sprintf("hls_720p=%d", boolToInt(contains(job.Request.Presets, "hls_720p"))),
		fmt.Sprintf("hls_1080p=%d", boolToInt(contains(job.Request.Presets, "hls_1080p"))),
		fmt.Sprintf("SCALE_FILTER=%s", scaleFilter),
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
	postToCallback(job, "success", "")
	cleanUpJob(job)
}

func handleJobFailure(job Job, errorMsg string) {
	log.Printf("[jid: %s] Job failed. Error: %s\n", job.JobID, errorMsg)

	postToCallback(job, "failure", errorMsg)
	cleanUpJob(job)
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
	os.RemoveAll("/tmp/" + job.JobID)
	JobMutex.Lock()
	delete(RunningJobs, job.JobID)
	JobMutex.Unlock()
}
