package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"math/rand"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// Struct for the POST request to /convert
type ConvertRequest struct {
	S3VideoURI  string   `json:"s3_video_uri"`
	S3HLSDirURI string   `json:"s3_hls_dir_uri"`
	Presets     []string `json:"presets"`
	ID          string   `json:"id"`
	CallbackURL string   `json:"callback_url"`
}

// Handler for GET /
func handleIndex(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Gumroad: MediaConverter.")
}

// AuthenticateRequest checks the Authorization header and returns an error if authentication fails
func AuthenticateRequest(r *http.Request) error {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return fmt.Errorf("authorization header is required")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || parts[0] != "Basic" {
		return fmt.Errorf("invalid Authorization header format")
	}

	payload, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("invalid Authorization header encoding")
	}

	pair := strings.SplitN(string(payload), ":", 2)
	if len(pair) != 2 || pair[1] != "" {
		return fmt.Errorf("invalid Authorization header content")
	}

	if pair[0] != os.Getenv("API_KEY") {
		return fmt.Errorf("unauthorized")
	}

	return nil
}

// Handler for POST /convert
func handleConvert(w http.ResponseWriter, r *http.Request) {
	if err := AuthenticateRequest(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req ConvertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate request
	if req.S3VideoURI == "" || req.S3HLSDirURI == "" || len(req.Presets) == 0 || req.ID == "" || req.CallbackURL == "" {
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	jobID := fmt.Sprintf("%x", rand.Int63())[:10]
	job := Job{
		JobID:     jobID,
		Request:   req,
		Status:    "pending",
		StartTime: time.Now(),
	}

	select {
	case JobQueue <- job:
		log.Printf("Job queued: %s\n", jobID)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"job_id": jobID})
	default:
		http.Error(w, "Too many jobs processing, please try again later", http.StatusTooManyRequests)
	}
}

// Handler for GET /status
func handleStatus(w http.ResponseWriter, r *http.Request) {
	if err := AuthenticateRequest(r); err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")

	var jobs = []Job{}
	if len(RunningJobs) > 0 {
		jobs = slices.Collect(maps.Values(RunningJobs))
	}

	response := map[string][]Job{
		"running_jobs": jobs,
	}
	json.NewEncoder(w).Encode(response)
}

func handleUp(w http.ResponseWriter, r *http.Request) {
	requiredEnvVars := []string{"PORT", "API_KEY", "MAX_SIMULTANEOUS_JOBS", "AWS_REGION", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY"}
	missingVars := []string{}

	for _, envVar := range requiredEnvVars {
		if os.Getenv(envVar) == "" {
			missingVars = append(missingVars, envVar)
		}
	}

	if len(missingVars) > 0 {
		errorMsg := fmt.Sprintf("Missing required environment variables: %s", strings.Join(missingVars, ", "))
		http.Error(w, errorMsg, http.StatusInternalServerError)
		return
	}

	fmt.Fprintln(w, "Ok.")
}

func startWebServer(port string) error {
	logRequests := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := fmt.Sprintf("%x", rand.Int63())[:10]
			log.Printf("[rid: %s] Processing %s request for %s from %s\n", requestID, r.Method, r.URL.Path, r.RemoteAddr)

			start := time.Now()
			next.ServeHTTP(w, r)

			duration := time.Since(start)
			log.Printf("[rid: %s] Request completed - Duration: %.4fms\n", requestID, float64(duration.Nanoseconds())/1e6)
		})
	}

	r := mux.NewRouter()
	r.Use(logRequests)
	r.HandleFunc("/", handleIndex).Methods("GET")
	r.HandleFunc("/convert", handleConvert).Methods("POST")
	r.HandleFunc("/status", handleStatus).Methods("GET")
	r.HandleFunc("/up", handleUp).Methods("GET")

	fmt.Println("Starting web server on " + port)
	return http.ListenAndServe(":"+port, r)
}
