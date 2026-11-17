# SSE MCP test server

This is a test server for verifying SSE-mode external MCP functionality.

## Usage

### 1. Start the test server

```bash
cd cmd/test-sse-mcp-server
go run main.go
```

The server starts at `http://127.0.0.1:8082` and provides the following endpoints:
- `GET /sse` - SSE event-stream endpoint
- `POST /message` - message-receiving endpoint

### 2. Add the configuration in Hoda Engine

Add an external MCP configuration in the web UI using the following JSON:

```json
{
  "test-sse-mcp": {
    "transport": "sse",
    "url": "http://127.0.0.1:8082/sse",
    "description": "SSE MCP test server",
    "timeout": 30
  }
}
```

### 3. Test the functionality

The test server provides two test tools:

1. **test_echo** - echoes the input text
   - Parameter: `text` (string) - the text to echo

2. **test_add** - computes the sum of two numbers
   - Parameter: `a` (number) - the first number
   - Parameter: `b` (number) - the second number

## How it works

1. The client establishes an SSE connection via `GET /sse` and receives events pushed by the server
2. The client sends MCP protocol messages via `POST /message`
3. After processing a message, the server pushes the response over the SSE connection

## Logging

The server outputs the following logs:
- SSE client connect/disconnect
- Received requests (method name and ID)
- Tool-call details

