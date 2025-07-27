package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestConfigurationParsing(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "confsync-test")

	// Build the test binary
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Failed to build test binary: %v", err)
	}

	tests := []struct {
		name        string
		args        []string
		envVars     map[string]string
		expectError bool
		checkLog    func(output string) error
	}{
		{
			name: "EnvironmentVariableUsed",
			args: []string{},
			envVars: map[string]string{
				"CONFSYNC_URL":       "http://example.com/files",
				"CONFSYNC_LOCAL_DIR": "/tmp/env-test-dir",
			},
			expectError: false,
			checkLog: func(output string) error {
				if !strings.Contains(output, "Local directory: /tmp/env-test-dir") {
					return fmt.Errorf("expected 'Local directory: /tmp/env-test-dir' in output, got: %s", output)
				}
				return nil
			},
		},
		{
			name: "CommandLineFlagOverridesEnv",
			args: []string{"-url", "http://example.com/files", "-dir", "/tmp/flag-test-dir"},
			envVars: map[string]string{
				"CONFSYNC_LOCAL_DIR": "/tmp/env-test-dir",
			},
			expectError: false,
			checkLog: func(output string) error {
				if !strings.Contains(output, "Local directory: /tmp/flag-test-dir") {
					return fmt.Errorf("expected 'Local directory: /tmp/flag-test-dir' in output, got: %s", output)
				}
				return nil
			},
		},
		{
			name: "MultipleEnvVarsWork",
			args: []string{},
			envVars: map[string]string{
				"CONFSYNC_URL":          "http://example.com/files",
				"CONFSYNC_LOCAL_DIR":    "/tmp/multi-env-dir",
				"CONFSYNC_FILE_PATTERN": ".*\\.yaml",
				"CONFSYNC_VERBOSE":      "true",
			},
			expectError: false,
			checkLog: func(output string) error {
				checks := []string{
					"Local directory: /tmp/multi-env-dir",
					"File pattern: .*\\.yaml",
					"Remote URL: http://example.com/files",
				}
				for _, check := range checks {
					if !strings.Contains(output, check) {
						return fmt.Errorf("expected '%s' in output, got: %s", check, output)
					}
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use context with timeout to avoid race conditions
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			// Create command with context for timeout
			cmd := exec.CommandContext(ctx, binaryPath, tt.args...)
			cmd.Env = os.Environ()

			// Set test environment variables
			for key, value := range tt.envVars {
				cmd.Env = append(cmd.Env, key+"="+value)
			}

			// Run command - will be cancelled by context timeout
			output, _ := cmd.CombinedOutput()

			outputStr := string(output)

			// Check if we got the expected logs before the timeout/error
			if tt.checkLog != nil {
				if err := tt.checkLog(outputStr); err != nil {
					t.Errorf("Log check failed: %v\nOutput: %s", err, outputStr)
				}
			}
		})
	}
}
