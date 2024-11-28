package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

func createTempDirectory(path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	return nil
}
