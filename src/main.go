package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const defaultPort = "7454"

var (
	awsClient           *s3.Client
	JobQueue            chan Job
	MaxSimultaneousJobs int
	port                string
)

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	cfg, err := awsConfig.LoadDefaultConfig(context.TODO(),
		awsConfig.WithRegion(os.Getenv("AWS_REGION")),
	)
	if err != nil {
		log.Fatalf("Error initializing AWS config: %v", err)
	}

	awsClient = s3.NewFromConfig(cfg)

	envMaxJobs := os.Getenv("MAX_SIMULTANEOUS_JOBS")
	if envMaxJobs == "" {
		log.Fatalf("Error: MAX_SIMULTANEOUS_JOBS is not set")
	}

	MaxSimultaneousJobs, err = strconv.Atoi(envMaxJobs)
	if err != nil {
		log.Fatalf("Error: converting MAX_SIMULTANEOUS_JOBS to int: %v", err)
	}
	JobQueue = make(chan Job, MaxSimultaneousJobs)

	webhookSecret := os.Getenv("WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Fatalf("Error: WEBHOOK_SECRET is not set")
	}

	port = os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i := 0; i < MaxSimultaneousJobs; i++ {
		go jobWorker(ctx)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- startWebServer(port)
	}()

	select {
	case <-sigChan:
		log.Println("Shutdown signal received")
	case err := <-serverErr:
		log.Printf("Server error: %v", err)
	}
}
