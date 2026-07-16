# Application Insights Emulator

A small local HTTP emulator for Application Insights ingestion. It accepts telemetry batches on `POST /v2/track`, groups records by telemetry type, and appends each record to a separate log file under `telemetry/`.
Uses only stdlib.

## What it does

- Accepts JSON telemetry envelopes from an Application Insights SDK.
- Detects the telemetry kind from `data.baseType`.
- Writes each item to one file per telemetry type.
- Creates the storage directory automatically.

## Getting started

### Prerequisites

- Go 1.22 or newer

### Run locally, with all available options

```powershell
go run . --port 61050 --storage-dir ingestion-data --log-level info
```

The emulator listens on `localhost` only, and on port `6060` by default.

### Use a connection string

Set `APPLICATIONINSIGHTS_CONNECTION_STRING` like this to send telemetry to the local emulator:

```powershell
$env:APPLICATIONINSIGHTS_CONNECTION_STRING = "InstrumentationKey=00000000-0000-0000-0000-000000000000;IngestionEndpoint=http://localhost:6060/"
```

If your SDK requires a full connection string including the track path, use the same ingestion endpoint and let the SDK append `/v2/track`.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `--port` | HTTP port for the ingest endpoint | `6060` |
| `--storage-dir` | Directory for the generated log files | `telemetry` |
| `--log-level` | Informational log level marker | `info` |

## Output files

The emulator creates one file per telemetry family:

```text
telemetry/
├── availability.log
├── dependencies.log
├── exceptions.log
├── events.log
├── metrics.log
├── pageviews.log
├── requests.log
└── traces.log
```

Unknown or custom telemetry types are written to a fallback file derived from their `data.baseType` value.

## Example request

```powershell
curl -X POST http://localhost:6060/v2/track ^
  -H "Content-Type: application/json" ^
  -d "[{\"name\":\"Microsoft.ApplicationInsights.Event\",\"iKey\":\"demo\",\"data\":{\"baseType\":\"EventData\",\"baseData\":{\"name\":\"hello\"}}}]"
```

If the request is valid, the emulator returns HTTP 200 and appends the envelope to `telemetry/events.log`.

## Health and status

- `GET /healthz` returns `200 OK` with `ok`.
- `GET /status` returns JSON with the configured port, storage directory, log level, and the current `.log` files found in the telemetry directory.

## Notes

- The emulator is intentionally lightweight and focuses on local inspection.
- It currently expects telemetry envelopes with `data.baseType`.
- Unsupported or malformed payloads return HTTP 400.
