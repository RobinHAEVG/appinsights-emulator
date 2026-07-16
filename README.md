# Application Insights Emulator

A small local HTTP emulator for Application Insights ingestion. It accepts telemetry batches on `POST /v2/track`, groups records by telemetry type, and appends each record to a separate log file under `telemetry/`.

## What it does

- Accepts JSON telemetry envelopes from an Application Insights SDK.
- Detects the telemetry kind from `data.baseType`.
- Writes each item to one file per telemetry type.
- Creates the storage directory automatically.

## Getting started

### Prerequisites

- Go 1.22 or newer

### Run locally, with all available options

```bash
appinsights-emulator --port 61050 --storageDir ingestion-data --logLevel info
```

The emulator listens on localhost only, and on port 6060 by default.

## Configuration

| Variable | Purpose | Default |
| --- | --- | --- |
| `--port` | HTTP port for the ingest endpoint | `6060` |
| `--storageDir` | Directory for the generated log files | `telemetry` |
| `--logLevel` | Informational log level marker | `info` |

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

```bash
curl -X POST http://localhost:6060/v2/track ^
  -H "Content-Type: application/json" ^
  -d "[{\"name\":\"Microsoft.ApplicationInsights.Event\",\"iKey\":\"demo\",\"data\":{\"baseType\":\"EventData\",\"baseData\":{\"name\":\"hello\"}}}]"
```

If the request is valid, the emulator returns HTTP 200 and appends the envelope to `telemetry/events.log`.

## Notes

- The emulator is intentionally lightweight and focuses on local inspection.
- It currently expects telemetry envelopes with `data.baseType`.
- Unsupported or malformed payloads return HTTP 400.
