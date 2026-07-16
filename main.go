package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

type config struct {
	port       int
	storageDir string
	logLevel   string
}

type envelope struct {
	Name string         `json:"name"`
	IKey string         `json:"iKey"`
	Time string         `json:"time"`
	Tags map[string]any `json:"tags"`
	Data telemetryData  `json:"data"`
}

type telemetryData struct {
	BaseType string          `json:"baseType"`
	BaseData json.RawMessage `json:"baseData"`
}

type telemetryWriter struct {
	storageDir string
	mu         sync.Mutex
}

type telemetryFileStatus struct {
	Name    string `json:"name"`
	Records int    `json:"records"`
	Bytes   int64  `json:"bytes"`
}

type statusResponse struct {
	OK             bool                  `json:"ok"`
	Port           int                   `json:"port"`
	StorageDir     string                `json:"storageDir"`
	LogLevel       string                `json:"logLevel"`
	TelemetryFiles []telemetryFileStatus `json:"telemetryFiles"`
}

var (
	port       int
	storageDir string
	logLevel   string
)

func main() {
	configureFlags()
	flag.Parse()

	cfg := config{
		port:       port,
		storageDir: storageDir,
		logLevel:   logLevel,
	}

	writer := &telemetryWriter{storageDir: cfg.storageDir}
	handler := newHandler(cfg, writer)

	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", cfg.port),
		Handler: handler,
	}

	log.Printf("appinsights emulator listening on http://localhost:%d", cfg.port)
	log.Printf("telemetry storage directory: %s", cfg.storageDir)
	if cfg.logLevel != "" {
		log.Printf("log level: %s", cfg.logLevel)
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func configureFlags() {
	portDefault := envInt("APPINSIGHTS_EMULATOR_PORT", 6060)
	storageDirDefault := envString("APPINSIGHTS_STORAGE_DIR", "telemetry")
	logLevelDefault := envString("LOG_LEVEL", "info")

	flag.IntVar(&port, "port", portDefault, "Port to listen on")
	flag.StringVar(&storageDir, "storage-dir", storageDirDefault, "Directory to store telemetry files")
	flag.StringVar(&logLevel, "log-level", logLevelDefault, "Log level (debug, info, warn, error)")
}

func envString(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}

	return parsed
}

func newHandler(cfg config, writer *telemetryWriter) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/track", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := ingestRequest(r.Body, writer); err != nil {
			log.Printf("ingest failed: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		files, err := collectTelemetryStatus(writer.storageDir)
		if err != nil {
			log.Printf("status failed: %v", err)
			http.Error(w, "failed to collect telemetry status", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusResponse{
			OK:             true,
			Port:           cfg.port,
			StorageDir:     cfg.storageDir,
			LogLevel:       cfg.logLevel,
			TelemetryFiles: files,
		})
	})

	return mux
}

func ingestRequest(body io.Reader, writer *telemetryWriter) error {
	payload, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("read request body: %w", err)
	}

	if len(payload) == 0 {
		return errors.New("request body is empty")
	}

	envelopes, err := decodeEnvelopes(payload)
	if err != nil {
		return err
	}

	for _, env := range envelopes {
		if err := writer.write(env); err != nil {
			return err
		}
	}

	return nil
}

func decodeEnvelopes(payload []byte) ([]envelope, error) {
	var envelopes []envelope
	if err := json.Unmarshal(payload, &envelopes); err == nil {
		return envelopes, validateEnvelopes(envelopes)
	}

	var single envelope
	if err := json.Unmarshal(payload, &single); err != nil {
		return nil, errors.New("request body must be a JSON array or object")
	}

	if err := validateEnvelopes([]envelope{single}); err != nil {
		return nil, err
	}

	return []envelope{single}, nil
}

func validateEnvelopes(envelopes []envelope) error {
	for index, env := range envelopes {
		if telemetryBaseType(env) == "" {
			return fmt.Errorf("envelope at index %d is missing data.baseType", index)
		}
	}

	return nil
}

func (w *telemetryWriter) write(env envelope) error {
	fileName := telemetryFileName(telemetryBaseType(env))
	line, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("marshal telemetry envelope: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(w.storageDir, 0o755); err != nil {
		return fmt.Errorf("create storage directory: %w", err)
	}

	path := filepath.Join(w.storageDir, fileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open telemetry file: %w", err)
	}
	defer file.Close()

	if _, err := file.Write(line); err != nil {
		return fmt.Errorf("write telemetry record: %w", err)
	}
	if _, err := file.Write([]byte("\n")); err != nil {
		return fmt.Errorf("write telemetry newline: %w", err)
	}

	return nil
}

func telemetryFileName(baseType string) string {
	switch normalizeBaseType(baseType) {
	case "eventdata":
		return "events.log"
	case "requestdata":
		return "requests.log"
	case "remotedependencydata":
		return "dependencies.log"
	case "exceptiondata":
		return "exceptions.log"
	case "metricdata":
		return "metrics.log"
	case "pageviewdata":
		return "pageviews.log"
	case "availabilitydata":
		return "availability.log"
	case "tracedata":
		return "traces.log"
	default:
		return normalizeBaseType(baseType) + ".log"
	}
}

func telemetryBaseType(env envelope) string {
	if value := strings.TrimSpace(env.Data.BaseType); value != "" {
		return value
	}

	return inferBaseTypeFromName(env.Name)
}

func inferBaseTypeFromName(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch {
	case lower == "microsoft.applicationinsights.event":
		return "EventData"
	case lower == "microsoft.applicationinsights.request":
		return "RequestData"
	case lower == "microsoft.applicationinsights.remotedependency":
		return "RemoteDependencyData"
	case lower == "microsoft.applicationinsights.exception":
		return "ExceptionData"
	case lower == "microsoft.applicationinsights.metric":
		return "MetricData"
	case lower == "microsoft.applicationinsights.pageview":
		return "PageViewData"
	case lower == "microsoft.applicationinsights.availability":
		return "AvailabilityData"
	case lower == "microsoft.applicationinsights.trace":
		return "TraceData"
	default:
		return ""
	}
}

func collectTelemetryStatus(storageDir string) ([]telemetryFileStatus, error) {
	entries, err := os.ReadDir(storageDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []telemetryFileStatus{}, nil
		}
		return nil, err
	}

	statuses := make([]telemetryFileStatus, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".log") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return nil, err
		}

		records, err := countLogRecords(filepath.Join(storageDir, entry.Name()))
		if err != nil {
			return nil, err
		}

		statuses = append(statuses, telemetryFileStatus{
			Name:    entry.Name(),
			Records: records,
			Bytes:   info.Size(),
		})
	}

	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})

	return statuses, nil
}

func countLogRecords(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	if len(data) == 0 {
		return 0, nil
	}

	return bytes.Count(data, []byte("\n")), nil
}

func normalizeBaseType(baseType string) string {
	value := strings.ToLower(strings.TrimSpace(baseType))
	value = nonAlphaNumeric.ReplaceAllString(value, "")
	if value == "" {
		return "unknown"
	}
	return value
}
