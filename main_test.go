package main

import (
	"bytes"
	"compress/gzip"
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

	req := httptest.NewRequest(http.MethodPost, "/v2/track", strings.NewReader(`[{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseData":{"name":"hello"}}}]`))
	accepted, err := ingestRequest(req, writer)
	if err != nil {
		t.Fatalf("ingestRequest returned error: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want %d", accepted, 1)
	}

	content, err := os.ReadFile(filepath.Join(dir, "events.log"))
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	if !strings.Contains(string(content), `"name":"Microsoft.ApplicationInsights.Event"`) {
		t.Fatalf("events.log does not contain expected telemetry record: %s", string(content))
	}
}

func TestIngestRequestGzip(t *testing.T) {
	dir := t.TempDir()
	writer := &telemetryWriter{storageDir: dir}

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	_, _ = gz.Write([]byte(`[{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseData":{"name":"hello"}}}]`))
	_ = gz.Close()

	req := httptest.NewRequest(http.MethodPost, "/v2/track", bytes.NewReader(compressed.Bytes()))
	req.Header.Set("Content-Encoding", "gzip, deflate")

	accepted, err := ingestRequest(req, writer)
	if err != nil {
		t.Fatalf("ingestRequest gzip returned error: %v", err)
	}
	if accepted != 1 {
		t.Fatalf("accepted = %d, want %d", accepted, 1)
	}

	if _, err := os.Stat(filepath.Join(dir, "events.log")); err != nil {
		t.Fatalf("expected events.log to be written, got error: %v", err)
	}
}

func TestIngestRequestJSONStream(t *testing.T) {
	dir := t.TempDir()
	writer := &telemetryWriter{storageDir: dir}

	body := strings.NewReader(
		`{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseData":{"name":"hello"}}}` + "\n" +
			`{"name":"Microsoft.ApplicationInsights.Trace","iKey":"demo","data":{"baseData":{"message":"m"}}}`,
	)
	req := httptest.NewRequest(http.MethodPost, "/v2/track", body)

	accepted, err := ingestRequest(req, writer)
	if err != nil {
		t.Fatalf("ingestRequest JSON stream returned error: %v", err)
	}
	if accepted != 2 {
		t.Fatalf("accepted = %d, want %d", accepted, 2)
	}

	if _, err := os.Stat(filepath.Join(dir, "events.log")); err != nil {
		t.Fatalf("expected events.log to be written, got error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "traces.log")); err != nil {
		t.Fatalf("expected traces.log to be written, got error: %v", err)
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

func TestTrackEndpointReturnsBreezeResponse(t *testing.T) {
	dir := t.TempDir()
	writer := &telemetryWriter{storageDir: dir}
	handler := newHandler(config{port: 6060, storageDir: dir, logLevel: "info"}, writer)

	req := httptest.NewRequest(http.MethodPost, "/v2/track", strings.NewReader(`[{"name":"Microsoft.ApplicationInsights.Event","iKey":"demo","data":{"baseData":{"name":"hello"}}}]`))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("track status = %d, want %d", rr.Code, http.StatusOK)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"itemsReceived":1`) {
		t.Fatalf("track body missing itemsReceived: %s", body)
	}
	if !strings.Contains(body, `"itemsAccepted":1`) {
		t.Fatalf("track body missing itemsAccepted: %s", body)
	}
}
