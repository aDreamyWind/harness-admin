package httpserver

import (
	"context"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpLegacyTransport implements HTTP+SSE without rejecting extension methods
// before the MCP server can dispatch them. The SDK's SSEServerTransport does
// that precheck against its fixed built-in method table.
type mcpLegacyTransport struct {
	endpoint  string
	response  http.ResponseWriter
	incoming  chan jsonrpc.Message
	done      chan struct{}
	mu        sync.Mutex
	connected bool
	closed    bool
}

func newMCPLegacyTransport(endpoint string, response http.ResponseWriter) *mcpLegacyTransport {
	return &mcpLegacyTransport{endpoint: endpoint, response: response,
		incoming: make(chan jsonrpc.Message, 100), done: make(chan struct{})}
}

var _ mcp.Transport = (*mcpLegacyTransport)(nil)
var _ mcp.Connection = (*mcpLegacyTransport)(nil)

func (t *mcpLegacyTransport) Connect(context.Context) (mcp.Connection, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.connected {
		return nil, fmt.Errorf("SSE transport already connected")
	}
	t.connected = true
	if err := t.writeEvent("endpoint", []byte(t.endpoint)); err != nil {
		return nil, err
	}
	return t, nil
}

func (t *mcpLegacyTransport) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(writer, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	data, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(writer, "failed to read body", http.StatusBadRequest)
		return
	}
	message, err := jsonrpc.DecodeMessage(data)
	if err != nil {
		http.Error(writer, "failed to parse JSON-RPC", http.StatusBadRequest)
		return
	}
	select {
	case t.incoming <- message:
		writer.WriteHeader(http.StatusAccepted)
	case <-t.done:
		http.Error(writer, "session closed", http.StatusBadRequest)
	case <-request.Context().Done():
		return
	}
}

func (t *mcpLegacyTransport) SessionID() string { return "" }

func (t *mcpLegacyTransport) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case message := <-t.incoming:
		return message, nil
	case <-t.done:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *mcpLegacyTransport) Write(ctx context.Context, message jsonrpc.Message) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	data, err := jsonrpc.EncodeMessage(message)
	if err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return io.EOF
	}
	return t.writeEvent("message", data)
}

func (t *mcpLegacyTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		close(t.done)
	}
	return nil
}

// writeEvent requires t.mu; only the GET stream writes to its response.
func (t *mcpLegacyTransport) writeEvent(name string, data []byte) error {
	if _, err := fmt.Fprintf(t.response, "event: %s\ndata: %s\n\n", name, data); err != nil {
		return err
	}
	if flusher, ok := t.response.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}
