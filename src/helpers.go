package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
)

func boolToInt(val bool) int {
	if val {
		return 1
	}
	return 0
}

// Zero-padded so the slice can't go out of range on a small value.
func randomID() string {
	return fmt.Sprintf("%010x", rand.Int63())[:10]
}

func createTempDirectory(path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %v", err)
	}
	return nil
}
