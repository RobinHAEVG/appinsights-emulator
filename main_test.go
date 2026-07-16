package main

import (
	"net/http"
	"net/http/httptest"
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

	body := strings.NewReader(`[{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseData":{"name":"hello"}}}]`)
	if err := ingestRequest(body, writer); err != nil {
		t.Fatalf("ingestRequest returned error: %v", err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "events.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if !strings.Contains(string(content), `"name":"Microsoft.ApplicationInsights.Event"`) {
		t.Fatalf("events.log does not contain expected telemetry record: %s", string(content))
	}
}

func TestTelemetryBaseTypeFallback(t *testing.T) {
	got := telemetryBaseType(envelope{Name: "Microsoft.ApplicationInsights.Event"})
	if got != "EventData" {
		t.Fatalf("telemetryBaseType fallback = %q, want %q", got, "EventData")
	}
}

func TestHealthAndStatusEndpoints(t *testing.T) {
	dir := t.TempDir()
	writer := &telemetryWriter{storageDir: dir}
	if err := writer.write(envelope{Data: telemetryData{BaseType: "EventData"}}); err != nil {
		t.Fatalf("writer.write returned error: %v", err)
	}

	handler := newHandler(config{port: 6060, storageDir: dir, logLevel: "info"}, writer)

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("healthz status = %d, want %d", rr.Code, http.StatusOK)
		}
		if strings.TrimSpace(rr.Body.String()) != "ok" {
			t.Fatalf("healthz body = %q, want %q", rr.Body.String(), "ok")
		}
	})

	t.Run("status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("status code = %d, want %d", rr.Code, http.StatusOK)
		}
		body := rr.Body.String()
		if !strings.Contains(body, `"ok":true`) {
			t.Fatalf("status body missing ok flag: %s", body)
		}
		if !strings.Contains(body, `"records":1`) {
			t.Fatalf("status body missing record count: %s", body)
		}
	})
}
