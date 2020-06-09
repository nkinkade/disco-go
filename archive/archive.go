package archive

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
)

// Sample comment.
type Sample struct {
	Timestamp int64  `json:"timestamp"`
	Value     uint64 `json:"value"`
}

// Model comment.
type Model struct {
	Experiment string   `json:"experiment"`
	Hostname   string   `json:"hostname"`
	Metric     string   `json:"metric"`
	Samples    []Sample `json:"sample"`
}

// Write comment.
func Write(m Model, interval uint64) {
	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/02"), m.Hostname)
	err := os.MkdirAll(dirs, 0755)
	if err != nil {
		log.Fatalf("Failed to create archive output directory (%v): %v", dirs, err)
	}

	// Calculate the start time, which will be Now() - interval, and then format
	// the archive file name based on the calculated values.
	startTime := time.Now().Add(time.Duration(interval) * -time.Second)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")
	endTimeStr := time.Now().Format("2006-01-02T15:04:05")
	fileName := fmt.Sprintf("%v-to-%v-switch.json", startTimeStr, endTimeStr)
	filePath := fmt.Sprintf("%v/%v", dirs, fileName)

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open archive file (%v): %v", filePath, err)
	}
	defer f.Close()

	data, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		log.Fatalf("Failed to marshal archive model for writing: %v", err)
	}
	_, err = f.Write(data)
	if err != nil {
		log.Fatalf("Failed to write archive model data to file (%v): %v", filePath, err)
	}
}
