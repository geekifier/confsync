package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFileEntryParsing(t *testing.T) {
	jsonData := `[
		{
			"name": "config.yaml",
			"type": "file",
			"mtime": "Sun, 27 Jul 2025 04:23:20 GMT",
			"size": 167
		},
		{
			"name": "test.json",
			"type": "file", 
			"mtime": "Sun, 27 Jul 2025 04:23:23 GMT",
			"size": 266
		}
	]`

	var entries []FileEntry
	err := json.Unmarshal([]byte(jsonData), &entries)
	if err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(entries) != 2 {
		t.Errorf("Expected 2 entries, got %d", len(entries))
	}

	if entries[0].Name != "config.yaml" {
		t.Errorf("Expected first file name to be 'config.yaml', got '%s'", entries[0].Name)
	}

	if entries[0].Size != 167 {
		t.Errorf("Expected first file size to be 167, got %d", entries[0].Size)
	}
}

func TestHealthEndpoint(t *testing.T) {
	config := Config{
		RemoteURL:       "http://example.com/files",
		LocalDir:        "./test-sync",
		FilePattern:     ".*",
		PollInterval:    time.Minute,
		UserAgent:       "test",
		ConnectTimeout:  30 * time.Second,
		DownloadTimeout: 0, // Unlimited
		MaxRetries:      3,
		RetryDelay:      5 * time.Second,
		Verbose:         false,
		HealthPort:      0, // Disable health server for test
	}

	app, err := NewConfsyncApp(config)
	if err != nil {
		t.Fatalf("Failed to create app: %v", err)
	}

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	app.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var health HealthStatus
	err = json.NewDecoder(w.Body).Decode(&health)
	if err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if health.Status == "" {
		t.Error("Expected health status to be set")
	}
}

func TestRegexMatching(t *testing.T) {
	config := Config{
		FilePattern: `^.*\.ya?ml$`,
	}

	app, err := NewConfsyncApp(config)
	if err != nil {
		t.Fatalf("Failed to create app: %v", err)
	}

	testCases := []struct {
		filename string
		expected bool
	}{
		{"config.yaml", true},
		{"settings.yml", true},
		{"data.json", false},
		{"test.txt", false},
		{"config.yaml.bak", false},
	}

	for _, tc := range testCases {
		result := app.fileRegex.MatchString(tc.filename)
		if result != tc.expected {
			t.Errorf("Pattern match for '%s': expected %v, got %v", tc.filename, tc.expected, result)
		}
	}
}
