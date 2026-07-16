package main

import (
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
	"strings"
	"sync"
)

const defaultPort = "6060"

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

var (
	port       int
	storageDir string
	logLevel   string
)

func main() {

	flag.IntVar(&port, "port", 6060, "Port to listen on")
	flag.StringVar(&storageDir, "storage-dir", "./telemetry", "Directory to store telemetry files")
	flag.StringVar(&logLevel, "log-level", "", "Log level (debug, info, warn, error)")
	flag.Parse()

	cfg := config{
		port:       port,
		storageDir: storageDir,
		logLevel:   logLevel,
	}

	writer := &telemetryWriter{storageDir: cfg.storageDir}

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

	server := &http.Server{
		Addr:    fmt.Sprintf("localhost:%d", cfg.port),
		Handler: mux,
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
		if strings.TrimSpace(env.Data.BaseType) == "" {
			return fmt.Errorf("envelope at index %d is missing data.baseType", index)
		}
	}

	return nil
}

func (w *telemetryWriter) write(env envelope) error {
	fileName := telemetryFileName(env.Data.BaseType)
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

func normalizeBaseType(baseType string) string {
	value := strings.ToLower(strings.TrimSpace(baseType))
	value = nonAlphaNumeric.ReplaceAllString(value, "")
	if value == "" {
		return "unknown"
	}
	return value
}
