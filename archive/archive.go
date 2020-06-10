package archive

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/m-lab/go/rtx"
)

// Sample represents the basic structure for metric samples.
type Sample struct {
	Timestamp int64  `json:"timestamp"`
	Value     uint64 `json:"value"`
}

// Model represents the structure of metric for DISCO.
type Model struct {
	Experiment string   `json:"experiment"`
	Hostname   string   `json:"hostname"`
	Metric     string   `json:"metric"`
	Samples    []Sample `json:"sample"`
}

// GetJSON accepts a Model object and returns marshalled JSON.
func GetJSON(m Model) []byte {
	data, err := json.MarshalIndent(m, "", "    ")
	rtx.Must(err, "Failed to marshal archive model to JSON")
	return data
}

// Write writes out JSON data to a file on disk.
func Write(hostname string, data []byte, interval uint64) {
	dirs := fmt.Sprintf("%v/%v", time.Now().Format("2006/01/02"), hostname)
	err := os.MkdirAll(dirs, 0755)
	rtx.Must(err, "Failed to create archive output directory")

	// Calculate the start time, which will be Now() - interval, and then format
	// the archive file name based on the calculated values.
	startTime := time.Now().Add(time.Duration(interval) * -time.Second)
	startTimeStr := startTime.Format("2006-01-02T15:04:05")
	endTimeStr := time.Now().Format("2006-01-02T15:04:05")
	fileName := fmt.Sprintf("%v-to-%v-switch.json", startTimeStr, endTimeStr)
	filePath := fmt.Sprintf("%v/%v", dirs, fileName)

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	rtx.Must(err, "Failed to open archive file")
	defer f.Close()

	_, err = f.Write(data)
	rtx.Must(err, "Failed to write archive model data to file")
}
