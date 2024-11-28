package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func downloadFromS3(ctx context.Context, s3URI string, localPath string) error {
	log.Printf("downloading from %s to %s\n", s3URI, localPath)

	bucket, key := parseS3URI(s3URI)

	output, err := awsClient.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to download video from S3: %v", err)
	}
	defer output.Body.Close()

	// Create a local file to save the video
	localFile, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("failed to create local file: %v", err)
	}
	defer localFile.Close()

	// Write video to local file
	_, err = io.Copy(localFile, output.Body)
	if err != nil {
		return fmt.Errorf("failed to save video: %v", err)
	}

	return nil
}

func uploadToS3(ctx context.Context, localDir string, s3URI string) error {
	log.Printf("uploading from %s to %s\n", localDir, s3URI)

	bucket, key := parseS3URI(s3URI)

	files, err := os.ReadDir(localDir)
	if err != nil {
		return fmt.Errorf("failed to read HLS output directory: %v", err)
	}

	for _, file := range files {
		filePath := filepath.Join(localDir, file.Name())
		fileContent, err := os.Open(filePath)
		if err != nil {
			return fmt.Errorf("failed to open HLS file: %v", err)
		}
		defer fileContent.Close()

		_, err = awsClient.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(filepath.Join(key, file.Name())),
			Body:   fileContent,
			ACL:    types.ObjectCannedACLPublicRead,
		})
		if err != nil {
			return fmt.Errorf("failed to upload HLS file to S3: %v", err)
		}
	}

	return nil
}

func parseS3URI(s3URI string) (bucket string, key string) {
	if !strings.HasPrefix(s3URI, "s3://") {
		return "", ""
	}
	parts := strings.SplitN(strings.TrimPrefix(s3URI, "s3://"), "/", 2)
	if len(parts) < 2 {
		return "", ""
	}
	bucket = parts[0]
	key = parts[1]
	return
}
