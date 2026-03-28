# JSON Logging in Stunner

Stunner supports JSON-formatted logging through redirection of the standard `log` package to Go's `slog` with JSON handler.

## How it Works

Stunner uses the Pion logging framework, which internally uses Go's standard `log` package. By redirecting the standard log output to `slog` with JSON formatting, all Stunner logs are automatically converted to JSON format.

## Usage

### Command Line Flag

Enable JSON logging using the `--json-log` or `-j` flag:

```bash
stunnerd --json-log -l all:INFO
```

### Environment Variable

You can also enable JSON logging using the `STUNNER_JSON_LOG` environment variable:

```bash
export STUNNER_JSON_LOG=true
stunnerd -l all:INFO
```

Or set it inline:

```bash
STUNNER_JSON_LOG=true stunnerd -l all:INFO
```

### Programmatic Usage

If you're using Stunner as a library, you can set up JSON logging before creating the Stunner instance:

```go
package main

import (
    "log"
    "log/slog"
    "os"
    
    "github.com/l7mp/stunner"
)

func main() {
    // Setup JSON logging
    handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelDebug,
    })
    
    // Redirect standard log to slog
    log.SetFlags(0)
    log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo))
    
    // Create Stunner instance
    st := stunner.NewStunner(stunner.Options{
        Name:     "my-stunner",
        LogLevel: "all:INFO",
    })
    
    // ... rest of your code
}
```

## JSON Log Format

The JSON logs include the following fields:

- `time`: Timestamp in RFC3339 format
- `level`: Log level (INFO, WARN, ERROR, DEBUG, TRACE)
- `msg`: The log message
- Additional structured fields when available

### Example Output

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"Starting stunnerd id \"default/stunnerd-hostname\", STUNner v1.0.0"}
{"time":"2024-01-15T10:30:45.124Z","level":"INFO","msg":"New configuration available: \"default/stunnerd-hostname\""}
{"time":"2024-01-15T10:30:45.125Z","level":"INFO","msg":"listener default-listener (re)starting"}
```

## Benefits

1. **Structured Logging**: JSON format makes it easy to parse and analyze logs
2. **No Code Changes**: Works with existing Stunner codebase without modifications
3. **Standard Go Libraries**: Uses Go's built-in `slog` package
4. **Flexible**: Can be enabled via command line or environment variable
5. **Compatible**: Works with all existing Stunner logging features

## Integration with Log Aggregation

JSON logging makes it easy to integrate with log aggregation systems like:

- **ELK Stack** (Elasticsearch, Logstash, Kibana)
- **Fluentd/Fluent Bit**
- **Prometheus + Grafana**
- **Cloud logging services** (AWS CloudWatch, Google Cloud Logging, Azure Monitor)

## Example with Docker

```dockerfile
FROM stunner/stunner:latest

# Enable JSON logging
ENV STUNNER_JSON_LOG=true

# Run with JSON logging
CMD ["stunnerd", "--json-log", "-l", "all:INFO"]
```

## Example with Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: stunner
spec:
  template:
    spec:
      containers:
      - name: stunner
        image: stunner/stunner:latest
        env:
        - name: STUNNER_JSON_LOG
          value: "true"
        args:
        - "--json-log"
        - "-l"
        - "all:INFO"
```

## Testing

You can test JSON logging using the provided example:

```bash
go run examples/json-logging/main.go
```

This will output JSON-formatted logs demonstrating the feature. 