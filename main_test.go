package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTelemetryFileName(t *testing.T) {
	tests := map[string]string{
		"EventData":            "events.log",
		"RequestData":          "requests.log",
		"RemoteDependencyData": "dependencies.log",
		"ExceptionData":        "exceptions.log",
		"MetricData":           "metrics.log",
		"PageViewData":         "pageviews.log",
		"AvailabilityData":     "availability.log",
		"TraceData":            "traces.log",
		"Custom-Type":          "customtype.log",
	}

	for input, want := range tests {
		if got := telemetryFileName(input); got != want {
			t.Fatalf("telemetryFileName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestIngestRequestWritesTelemetry(t *testing.T) {
	dir := t.TempDir()
	writer := &telemetryWriter{storageDir: dir}

	body := strings.NewReader(`[{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseType":"EventData","baseData":{"name":"hello"}}}]`)
	if err := ingestRequest(body, writer); err != nil {
		t.Fatalf("ingestRequest returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "events.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if !strings.Contains(string(content), `"baseType":"EventData"`) {
		t.Fatalf("events.log does not contain expected telemetry record: %s", string(content))
	}
}
