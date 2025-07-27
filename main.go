package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// FileEntry represents a file entry from the remote directory listing
type FileEntry struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	MTime string `json:"mtime"`
	Size  int64  `json:"size"`
}

// Config holds the application configuration
type Config struct {
	RemoteURL       string        `flag:"url" env:"CONFSYNC_URL" default:"" description:"Remote server URL providing directory listing"`
	LocalDir        string        `flag:"dir" env:"CONFSYNC_LOCAL_DIR" default:"" description:"Local directory to sync files to"`
	FilePattern     string        `flag:"pattern" env:"CONFSYNC_FILE_PATTERN" default:".*" description:"Regex pattern to match files"`
	PollInterval    time.Duration `flag:"interval" env:"CONFSYNC_POLL_INTERVAL" default:"60s" description:"Polling interval"`
	UserAgent       string        `flag:"user-agent" env:"CONFSYNC_USER_AGENT" default:"confsync/1.0" description:"HTTP User-Agent header"`
	ConnectTimeout  time.Duration `flag:"connect-timeout" env:"CONFSYNC_CONNECT_TIMEOUT" default:"10s" description:"HTTP connection and listing timeout"`
	DownloadTimeout time.Duration `flag:"download-timeout" env:"CONFSYNC_DOWNLOAD_TIMEOUT" default:"0s" description:"Maximum download time per file (0 = unlimited)"`
	MaxRetries      int           `flag:"max-retries" env:"CONFSYNC_MAX_RETRIES" default:"3" description:"Maximum number of retries for failed requests"`
	RetryDelay      time.Duration `flag:"retry-delay" env:"CONFSYNC_RETRY_DELAY" default:"5s" description:"Base delay for exponential backoff retries"`
	Verbose         bool          `flag:"verbose" env:"CONFSYNC_VERBOSE" default:"false" description:"Enable verbose logging"`
	HealthPort      int           `flag:"health-port" env:"CONFSYNC_HEALTH_PORT" default:"8080" description:"Port for health check endpoint (0 to disable)"`
	DeleteFiles     bool          `flag:"delete" env:"CONFSYNC_DELETE" default:"false" description:"Enable automatic deletion of local files not on remote server"`
}

// HealthStatus represents the health status of the application
type HealthStatus struct {
	Status        string            `json:"status"`
	Timestamp     time.Time         `json:"timestamp"`
	LastSync      time.Time         `json:"last_sync,omitempty"`
	LastError     string            `json:"last_error,omitempty"`
	SyncedFiles   int64             `json:"synced_files"`
	TotalRequests int64             `json:"total_requests"`
	FailedSyncs   int64             `json:"failed_syncs"`
	Uptime        time.Duration     `json:"uptime"`
	Config        map[string]string `json:"config"`
}

// ConfsyncApp represents the main application
type ConfsyncApp struct {
	config         Config
	listingClient  *http.Client
	downloadClient *http.Client
	fileRegex      *regexp.Regexp
	fileCache      map[string]FileEntry
	startTime      time.Time
	lastSync       time.Time
	lastError      string
	syncedFiles    int64
	totalReqs      int64
	failedSyncs    int64
	mu             sync.RWMutex
	healthServer   *http.Server
	downloadCancel context.CancelFunc
	downloadCtx    context.Context
}

// NewConfsyncApp creates a new instance of the application
func NewConfsyncApp(config Config) (*ConfsyncApp, error) {
	regex, err := regexp.Compile(config.FilePattern)
	if err != nil {
		return nil, fmt.Errorf("invalid file pattern regex: %w", err)
	}

	// Create separate HTTP clients for listing and downloads
	listingClient := &http.Client{
		Timeout: config.ConnectTimeout,
	}

	downloadClient := &http.Client{
		// No timeout for downloads - we'll use context for cancellation
	}

	// Create download context that can be cancelled
	downloadCtx, downloadCancel := context.WithCancel(context.Background())

	return &ConfsyncApp{
		config:         config,
		listingClient:  listingClient,
		downloadClient: downloadClient,
		fileRegex:      regex,
		fileCache:      make(map[string]FileEntry),
		startTime:      time.Now(),
		downloadCtx:    downloadCtx,
		downloadCancel: downloadCancel,
	}, nil
}

// fetchDirectoryListing fetches the directory listing from the remote server
func (app *ConfsyncApp) fetchDirectoryListing() ([]FileEntry, error) {
	atomic.AddInt64(&app.totalReqs, 1)

	var entries []FileEntry
	var lastErr error

	for retry := 0; retry <= app.config.MaxRetries; retry++ {
		if retry > 0 {
			// Exponential backoff: base delay * 2^(retry-1)
			backoffDelay := time.Duration(int64(app.config.RetryDelay) * int64(1<<(retry-1)))
			if app.config.Verbose {
				log.Printf("Retrying request (attempt %d/%d) after %v", retry, app.config.MaxRetries, backoffDelay)
			}
			time.Sleep(backoffDelay)
		}

		req, err := http.NewRequest("GET", app.config.RemoteURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		req.Header.Set("User-Agent", app.config.UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := app.listingClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch directory listing: %w", err)
			continue
		}

		defer func() {
			if closeErr := resp.Body.Close(); closeErr != nil {
				log.Printf("Failed to close response body: %v", closeErr)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("server returned status %d: %s", resp.StatusCode, resp.Status)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			continue
		}

		if err := json.Unmarshal(body, &entries); err != nil {
			lastErr = fmt.Errorf("failed to parse JSON response: %w", err)
			continue
		}

		return entries, nil
	}

	app.setLastError(fmt.Sprintf("failed after %d retries: %v", app.config.MaxRetries, lastErr))
	return nil, fmt.Errorf("failed after %d retries: %w", app.config.MaxRetries, lastErr)
}

// downloadFile downloads a file from the remote server with context-based cancellation
func (app *ConfsyncApp) downloadFile(filename string) error {
	fileURL := strings.TrimSuffix(app.config.RemoteURL, "/") + "/" + filename

	// Create download context with timeout if specified
	ctx := app.downloadCtx
	if app.config.DownloadTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(app.downloadCtx, app.config.DownloadTimeout)
		defer cancel()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", fileURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request for %s: %w", filename, err)
	}

	req.Header.Set("User-Agent", app.config.UserAgent)

	resp, err := app.downloadClient.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("download of %s was cancelled", filename)
		}
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("download of %s timed out after %v", filename, app.config.DownloadTimeout)
		}
		return fmt.Errorf("failed to download %s: %w", filename, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			log.Printf("Failed to close response body: %v", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: server returned status %d", filename, resp.StatusCode)
	}

	localPath := filepath.Join(app.config.LocalDir, filename)
	localDir := filepath.Dir(localPath)

	// Create directory if it doesn't exist
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", localDir, err)
	}

	// Create temporary file first
	tempPath := localPath + ".tmp"
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return fmt.Errorf("failed to create temporary file %s: %w", tempPath, err)
	}

	// Copy content to temporary file with context cancellation support
	_, err = io.Copy(tempFile, resp.Body)
	if closeErr := tempFile.Close(); closeErr != nil {
		log.Printf("Failed to close temporary file: %v", closeErr)
	}
	if err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Printf("Failed to remove temporary file %s: %v", tempPath, removeErr)
		}
		if ctx.Err() == context.Canceled {
			return fmt.Errorf("download of %s was cancelled during file write", filename)
		}
		return fmt.Errorf("failed to write to temporary file %s: %w", tempPath, err)
	}

	// Atomically move temporary file to final location
	if err := os.Rename(tempPath, localPath); err != nil {
		if removeErr := os.Remove(tempPath); removeErr != nil {
			log.Printf("Failed to remove temporary file %s: %v", tempPath, removeErr)
		}
		return fmt.Errorf("failed to move temporary file to %s: %w", localPath, err)
	}

	if app.config.Verbose {
		log.Printf("Downloaded: %s", filename)
	}

	return nil
}

// syncFiles synchronizes files based on the directory listing
func (app *ConfsyncApp) syncFiles() error {
	// Cancel any ongoing downloads from previous sync
	app.downloadCancel()

	// Create new download context for this sync iteration
	app.downloadCtx, app.downloadCancel = context.WithCancel(context.Background())

	entries, err := app.fetchDirectoryListing()
	if err != nil {
		return err
	}

	newCache := make(map[string]FileEntry)
	filesToSync := make([]FileEntry, 0)
	var filesToRemove []string

	// Process entries and identify files to sync
	for _, entry := range entries {
		if entry.Type != "file" {
			continue
		}

		if !app.fileRegex.MatchString(entry.Name) {
			continue
		}

		newCache[entry.Name] = entry

		// Check if file needs to be synced (new or modified)
		if cachedEntry, exists := app.fileCache[entry.Name]; !exists || cachedEntry.MTime != entry.MTime || cachedEntry.Size != entry.Size {
			filesToSync = append(filesToSync, entry)
		}
	}

	// Identify files to remove (only if deletion is enabled and listing was successful)
	if app.config.DeleteFiles {
		// Scan local directory for files to potentially remove
		entries, err := os.ReadDir(app.config.LocalDir)
		if err != nil {
			log.Printf("Warning: could not scan local directory for cleanup: %v", err)
		} else {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}

				filename := entry.Name()

				// Only consider files that match our pattern
				if !app.fileRegex.MatchString(filename) {
					continue
				}

				// If file doesn't exist on remote, mark for removal
				if _, exists := newCache[filename]; !exists {
					filesToRemove = append(filesToRemove, filename)
				}
			}
		}
	}

	// Remove files BEFORE downloading new ones (safer approach)
	removedCount := 0
	for _, filename := range filesToRemove {
		localPath := filepath.Join(app.config.LocalDir, filename)
		if err := os.Remove(localPath); err != nil {
			log.Printf("Error removing %s: %v", localPath, err)
		} else {
			removedCount++
			if app.config.Verbose {
				log.Printf("Removed: %s", filename)
			}
		}
	}

	// Download new/modified files
	downloadedCount := 0
	for _, entry := range filesToSync {
		if err := app.downloadFile(entry.Name); err != nil {
			// Check if error is due to cancellation (next sync started)
			if strings.Contains(err.Error(), "cancelled") {
				log.Printf("Download of %s cancelled due to new sync iteration", entry.Name)
				break // Stop processing downloads as new sync has started
			}
			log.Printf("Error downloading %s: %v", entry.Name, err)
			continue
		}
		downloadedCount++
		atomic.AddInt64(&app.syncedFiles, 1)
	}

	// Update cache only after successful operations
	app.fileCache = newCache

	// Update sync status
	app.mu.Lock()
	app.lastSync = time.Now()
	if len(filesToSync) == 0 && len(filesToRemove) == 0 {
		app.lastError = "" // Clear error on successful sync with no changes
	}
	app.mu.Unlock()

	// Log summary
	if downloadedCount > 0 || removedCount > 0 {
		log.Printf("Sync complete: downloaded %d, removed %d files matching pattern '%s'",
			downloadedCount, removedCount, app.config.FilePattern)
	} else if app.config.Verbose {
		log.Printf("No changes detected")
	}

	return nil
}

// setLastError safely sets the last error message
func (app *ConfsyncApp) setLastError(err string) {
	app.mu.Lock()
	app.lastError = err
	if err != "" {
		atomic.AddInt64(&app.failedSyncs, 1)
	}
	app.mu.Unlock()
}

// getHealthStatus returns the current health status
func (app *ConfsyncApp) getHealthStatus() HealthStatus {
	app.mu.RLock()
	defer app.mu.RUnlock()

	status := "healthy"
	if app.lastError != "" {
		// Consider unhealthy if last error was recent (within 3 sync intervals)
		errorThreshold := time.Now().Add(-3 * app.config.PollInterval)
		if app.lastSync.Before(errorThreshold) {
			status = "unhealthy"
		} else {
			status = "degraded"
		}
	}

	return HealthStatus{
		Status:        status,
		Timestamp:     time.Now(),
		LastSync:      app.lastSync,
		LastError:     app.lastError,
		SyncedFiles:   atomic.LoadInt64(&app.syncedFiles),
		TotalRequests: atomic.LoadInt64(&app.totalReqs),
		FailedSyncs:   atomic.LoadInt64(&app.failedSyncs),
		Uptime:        time.Since(app.startTime),
		Config: map[string]string{
			"remote_url":       app.config.RemoteURL,
			"local_dir":        app.config.LocalDir,
			"file_pattern":     app.config.FilePattern,
			"poll_interval":    app.config.PollInterval.String(),
			"connect_timeout":  app.config.ConnectTimeout.String(),
			"download_timeout": app.config.DownloadTimeout.String(),
			"max_retries":      fmt.Sprintf("%d", app.config.MaxRetries),
			"retry_delay":      app.config.RetryDelay.String(),
		},
	}
}

// healthHandler handles health check requests
func (app *ConfsyncApp) healthHandler(w http.ResponseWriter, r *http.Request) {
	health := app.getHealthStatus()

	w.Header().Set("Content-Type", "application/json")

	// Set HTTP status based on health
	switch health.Status {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // Still return 200 for degraded
	case "unhealthy":
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		log.Printf("Failed to encode health response: %v", err)
	}
}

// readinessHandler handles readiness check requests
func (app *ConfsyncApp) readinessHandler(w http.ResponseWriter, r *http.Request) {
	// Check if we can reach the remote URL
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "HEAD", app.config.RemoteURL, nil)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "failed to create request",
		}); err != nil {
			log.Printf("Failed to encode readiness response: %v", err)
		}
		return
	}

	req.Header.Set("User-Agent", app.config.UserAgent)

	resp, err := app.listingClient.Do(req)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		if err := json.NewEncoder(w).Encode(map[string]string{
			"status": "not ready",
			"error":  "remote server unreachable",
		}); err != nil {
			log.Printf("Failed to encode readiness response: %v", err)
		}
		return
	}
	if closeErr := resp.Body.Close(); closeErr != nil {
		log.Printf("Failed to close response body: %v", closeErr)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status": "ready",
	}); err != nil {
		log.Printf("Failed to encode readiness response: %v", err)
	}
}

// startHealthServer starts the health check HTTP server
func (app *ConfsyncApp) startHealthServer() error {
	if app.config.HealthPort <= 0 {
		return nil // Health server disabled
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", app.healthHandler)
	mux.HandleFunc("/health/live", app.healthHandler)
	mux.HandleFunc("/health/ready", app.readinessHandler)

	// Simple metrics endpoint
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		health := app.getHealthStatus()
		w.Header().Set("Content-Type", "text/plain")

		metrics := []string{
			"# HELP confsync_synced_files_total Total number of synced files\n",
			"# TYPE confsync_synced_files_total counter\n",
			fmt.Sprintf("confsync_synced_files_total %d\n", health.SyncedFiles),
			"# HELP confsync_requests_total Total number of requests to remote server\n",
			"# TYPE confsync_requests_total counter\n",
			fmt.Sprintf("confsync_requests_total %d\n", health.TotalRequests),
			"# HELP confsync_failed_syncs_total Total number of failed sync attempts\n",
			"# TYPE confsync_failed_syncs_total counter\n",
			fmt.Sprintf("confsync_failed_syncs_total %d\n", health.FailedSyncs),
			"# HELP confsync_uptime_seconds Uptime in seconds\n",
			"# TYPE confsync_uptime_seconds gauge\n",
			fmt.Sprintf("confsync_uptime_seconds %f\n", health.Uptime.Seconds()),
		}

		for _, metric := range metrics {
			if _, err := fmt.Fprint(w, metric); err != nil {
				log.Printf("Failed to write metric: %v", err)
				return
			}
		}
	})

	app.healthServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", app.config.HealthPort),
		Handler: mux,
	}

	go func() {
		log.Printf("Health server starting on port %d", app.config.HealthPort)
		if err := app.healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Health server error: %v", err)
		}
	}()

	return nil
}

// Run starts the synchronization loop
func (app *ConfsyncApp) Run() {
	log.Printf("Starting confsync")
	log.Printf("Remote URL: %s", app.config.RemoteURL)
	log.Printf("Local directory: %s", app.config.LocalDir)
	log.Printf("File pattern: %s", app.config.FilePattern)
	log.Printf("Poll interval: %v", app.config.PollInterval)

	// Ensure local directory exists
	if err := os.MkdirAll(app.config.LocalDir, 0755); err != nil {
		log.Fatalf("Failed to create local directory %s: %v", app.config.LocalDir, err)
	}

	// Start health server
	if err := app.startHealthServer(); err != nil {
		log.Fatalf("Failed to start health server: %v", err)
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Initial sync
	if err := app.syncFiles(); err != nil {
		log.Printf("Initial sync failed: %v", err)
		app.setLastError(fmt.Sprintf("Initial sync failed: %v", err))
	}

	// Start polling loop
	ticker := time.NewTicker(app.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := app.syncFiles(); err != nil {
				log.Printf("Sync failed: %v", err)
				app.setLastError(fmt.Sprintf("Sync failed: %v", err))
			}
		case sig := <-sigChan:
			log.Printf("Received signal %v, shutting down gracefully...", sig)

			// Cancel any ongoing downloads
			app.downloadCancel()

			// Shutdown health server
			if app.healthServer != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if err := app.healthServer.Shutdown(ctx); err != nil {
					log.Printf("Health server shutdown error: %v", err)
				}
			}

			log.Printf("Shutdown complete")
			return
		}
	}
}

// parseFlags parses command line flags and environment variables using struct tags
func parseFlags() Config {
	var config Config

	// Set defaults and register flags using reflection
	v := reflect.ValueOf(&config).Elem()
	t := reflect.TypeOf(config)

	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		flagName := fieldType.Tag.Get("flag")
		defaultValue := fieldType.Tag.Get("default")
		description := fieldType.Tag.Get("description")

		if flagName == "" {
			continue
		}

		// Register flags with default values based on field type
		switch field.Kind() {
		case reflect.String:
			field.SetString(defaultValue)
			flag.StringVar((*string)(field.Addr().UnsafePointer()), flagName, defaultValue, description)

		case reflect.Int:
			if intVal, err := strconv.Atoi(defaultValue); err == nil {
				field.SetInt(int64(intVal))
				flag.IntVar((*int)(field.Addr().UnsafePointer()), flagName, intVal, description)
			}

		case reflect.Bool:
			if boolVal, err := strconv.ParseBool(defaultValue); err == nil {
				field.SetBool(boolVal)
				flag.BoolVar((*bool)(field.Addr().UnsafePointer()), flagName, boolVal, description)
			}

		case reflect.TypeOf(time.Duration(0)).Kind():
			if field.Type() == reflect.TypeOf(time.Duration(0)) {
				if durVal, err := time.ParseDuration(defaultValue); err == nil {
					field.Set(reflect.ValueOf(durVal))
					flag.DurationVar((*time.Duration)(field.Addr().UnsafePointer()), flagName, durVal, description)
				}
			}
		}
	}

	// Parse command line flags first
	flag.Parse()

	// Build a set of explicitly provided flags from command line arguments
	explicitFlags := make(map[string]bool)
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(arg, "-")
			// Handle -flag=value format
			if eqIndex := strings.Index(flagName, "="); eqIndex != -1 {
				flagName = flagName[:eqIndex]
			}
			explicitFlags[flagName] = true
		}
	}

	// Override with environment variables if they exist (env vars take precedence over defaults but not over explicit flags)
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		fieldType := t.Field(i)

		flagName := fieldType.Tag.Get("flag")
		envName := fieldType.Tag.Get("env")

		if flagName == "" || envName == "" {
			continue
		}

		// Only override if flag was not explicitly set and env var exists
		if !explicitFlags[flagName] && os.Getenv(envName) != "" {
			envValue := os.Getenv(envName)

			switch field.Kind() {
			case reflect.String:
				field.SetString(envValue)

			case reflect.Int:
				if intVal, err := strconv.Atoi(envValue); err == nil {
					field.SetInt(int64(intVal))
				}

			case reflect.Bool:
				if boolVal, err := strconv.ParseBool(envValue); err == nil {
					field.SetBool(boolVal)
				}

			case reflect.TypeOf(time.Duration(0)).Kind():
				if field.Type() == reflect.TypeOf(time.Duration(0)) {
					if durVal, err := time.ParseDuration(envValue); err == nil {
						field.Set(reflect.ValueOf(durVal))
					}
				}
			}
		}
	}

	return config
}

func main() {
	config := parseFlags()

	if config.RemoteURL == "" {
		log.Fatal("Remote URL is required. Use -url flag or CONFSYNC_URL environment variable")
	}

	if config.LocalDir == "" {
		log.Fatal("Local directory is required. Use -dir flag or CONFSYNC_LOCAL_DIR environment variable")
	}

	app, err := NewConfsyncApp(config)
	if err != nil {
		log.Fatalf("Failed to create application: %v", err)
	}

	app.Run()
}
