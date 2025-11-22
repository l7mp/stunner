# JSON Logging Example

This example demonstrates how to use Stunner with JSON-formatted logging.

## Running the Example

```bash
go run main.go
```

## Expected Output

The example will output JSON-formatted logs like:

```json
{"time":"2024-01-15T10:30:45.123Z","level":"INFO","msg":"Starting Stunner with JSON logging"}
{"time":"2024-01-15T10:30:45.124Z","level":"INFO","msg":"Stunner configuration applied successfully"}
```

## How it Works

The example shows how to:
1. Set up a JSON handler using Go's `slog` package
2. Redirect the standard `log` package output to JSON format
3. Create a Stunner instance that outputs JSON logs

This approach works because Stunner (and the Pion libraries it uses) ultimately use Go's standard `log` package for logging. 