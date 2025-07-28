# Stunner JSON Logging Wrapper

This directory contains a wrapper implementation that converts all Stunner logs to JSON format.

## Overview

The `StunnerWrapper` class encapsulates the original `stunnerd` main functionality and provides:

- **Automatic JSON Logging**: All logs are automatically converted to JSON format
- **Structured Logging**: Log entries include structured fields for better parsing
- **Backward Compatibility**: Maintains the same command-line interface as the original `stunnerd`
- **Enhanced Observability**: Better integration with log aggregation systems

## Key Features

### JSON Logging Conversion
- All standard log output is converted to JSON format
- Structured fields for better log parsing and analysis
- Consistent log format across all Stunner components

### Wrapper Architecture
- `StunnerWrapper` class encapsulates the main functionality
- Clean separation between logging concerns and business logic
- Easy to extend with additional logging features

### Enhanced Logging
- Structured JSON output with consistent field names
- Better error context and debugging information
- Improved log levels and categorization

## Usage

The wrapper can be used as a drop-in replacement for the original `stunnerd`:

```bash
# Build the wrapper
go build -o stunnerd-wrapper ./cmd/wrapper

# Run with JSON logging (always enabled)
./stunnerd-wrapper --config k8s --verbose

# Run with custom configuration
./stunnerd-wrapper --config file://config.yaml --log all:DEBUG
```

## Log Output Format

All logs are output in JSON format with structured fields:

```json
{
  "time": "2024-01-15T10:30:45.123Z",
  "level": "INFO",
  "msg": "Starting stunnerd with JSON logging wrapper",
  "id": "default/stunnerd-hostname",
  "buildInfo": "dev (n/a) <unknown>"
}
```

## Implementation Details

### StunnerWrapper Class
- `SetupJSONLogging()`: Configures JSON logging for all output
- `InitializeStunner()`: Sets up the Stunner instance with logging
- `LoadConfiguration()`: Handles configuration loading with JSON logging
- `StartMainLoop()`: Runs the main event loop with enhanced logging
- `Close()`: Cleanup and resource management

### Log Conversion
- `slogWriter`: Custom writer that converts standard log output to JSON
- Automatic redirection of all log output to JSON format
- Structured field extraction from log messages

## Benefits

1. **Better Observability**: JSON logs are easier to parse and analyze
2. **Log Aggregation**: Better integration with ELK stack, Splunk, etc.
3. **Debugging**: Structured fields make debugging easier
4. **Monitoring**: Better integration with monitoring systems
5. **Compliance**: Structured logging helps with audit requirements

## Migration

To migrate from the original `stunnerd` to the wrapper:

1. Replace the binary with the wrapper version
2. No configuration changes required
3. All existing command-line options work the same
4. JSON logging is automatically enabled

## Development

The wrapper approach allows for easy extension:

- Add custom log fields
- Implement log filtering
- Add log rotation
- Integrate with external logging services
- Add metrics and monitoring hooks 