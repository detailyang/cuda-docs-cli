package nvidia_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/detailyang/cuda-docs-cli/internal/nvidia"
)

func TestParseResponseAcceptsStreamableHTTPSSE(t *testing.T) {
	body := []byte("event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[]}}\n\n")

	result, err := nvidia.ParseResponse(body)
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}

	var got struct {
		Tools []nvidia.Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &got); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if len(got.Tools) != 0 {
		t.Fatalf("tools length = %d, want 0", len(got.Tools))
	}
}

func TestParseResponseAcceptsLargeSingleLineSSEData(t *testing.T) {
	largeText := strings.Repeat("CUDA", 20_000)
	message, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result":  map[string]any{"text": largeText},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := nvidia.ParseResponse([]byte("data: " + string(message) + "\n\n"))
	if err != nil {
		t.Fatalf("ParseResponse() error = %v", err)
	}
	if !bytes.Contains(result, []byte(largeText[:100])) {
		t.Fatal("large result text was not preserved")
	}
}

func TestToolJSONPreservesUnmodeledInputSchemaFields(t *testing.T) {
	encoded := []byte(`{"name":"search","annotations":{"readOnlyHint":true},"inputSchema":{"type":"object","properties":{"scope":{"type":"string","enum":["guide","api"]}},"additionalProperties":false}}`)
	var tool nvidia.Tool
	if err := json.Unmarshal(encoded, &tool); err != nil {
		t.Fatal(err)
	}

	roundTrip, err := json.Marshal(tool)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(roundTrip, []byte(`"enum":["guide","api"]`)) || !bytes.Contains(roundTrip, []byte(`"additionalProperties":false`)) {
		t.Fatalf("tool JSON lost schema fields: %s", roundTrip)
	}
	if !bytes.Contains(roundTrip, []byte(`"annotations":{"readOnlyHint":true}`)) {
		t.Fatalf("tool JSON lost annotation fields: %s", roundTrip)
	}
}

func TestToolResultJSONPreservesUnmodeledContentFields(t *testing.T) {
	encoded := []byte(`{"content":[{"type":"text","text":"answer","annotations":{"audience":["user"]}}],"isError":false}`)
	var result nvidia.ToolResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	roundTrip, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(roundTrip, []byte(`"annotations":{"audience":["user"]}`)) {
		t.Fatalf("tool result JSON lost content fields: %s", roundTrip)
	}
}

func TestChooseSearchInvocationUsesAdvertisedStringField(t *testing.T) {
	tools := []nvidia.Tool{{
		Name:        "search_cuda_documentation",
		Description: "Search NVIDIA CUDA documentation",
		InputSchema: nvidia.InputSchema{
			Properties: map[string]nvidia.Property{"search_phrase": {Type: "string"}},
			Required:   []string{"search_phrase"},
		},
	}}

	name, arguments, err := nvidia.ChooseSearchInvocation(tools, "shared memory bank conflicts")
	if err != nil {
		t.Fatalf("ChooseSearchInvocation() error = %v", err)
	}
	if name != "search_cuda_documentation" {
		t.Fatalf("name = %q", name)
	}
	if got := arguments["search_phrase"]; got != "shared memory bank conflicts" {
		t.Fatalf("search_phrase = %#v", got)
	}
}

type staticToken string

func (token staticToken) Token(context.Context) (string, error) { return string(token), nil }

func TestClientCompletesSessionPaginationAndToolCall(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Errorf("Authorization = %q", got)
		}
		var message struct {
			ID     int64           `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		methods = append(methods, message.Method)
		switch message.Method {
		case "initialize":
			writer.Header().Set("Mcp-Session-Id", "session-1")
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"protocolVersion":"2025-03-26"}}`, message.ID)
		case "notifications/initialized":
			if got := request.Header.Get("Mcp-Session-Id"); got != "session-1" {
				t.Errorf("Mcp-Session-Id = %q", got)
			}
			writer.WriteHeader(http.StatusAccepted)
		case "tools/list":
			if strings.Contains(string(message.Params), "cursor") {
				fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"tools":[{"name":"second","inputSchema":{"type":"object"}}]}}`, message.ID)
				return
			}
			writer.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(writer, "event: message\ndata: {\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":{\"tools\":[{\"name\":\"first\",\"inputSchema\":{\"type\":\"object\"}}],\"nextCursor\":\"page-2\"}}\n\n", message.ID)
		case "tools/call":
			fmt.Fprintf(writer, `{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"answer"}]}}`, message.ID)
		default:
			http.Error(writer, "unexpected method", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := nvidia.NewClient(server.URL, staticToken("token-1"), server.Client())
	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "first" || tools[1].Name != "second" {
		t.Fatalf("tools = %#v", tools)
	}
	result, err := client.CallTool(context.Background(), "first", map[string]any{"query": "warp"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if got := result.Content[0].Text; got != "answer" {
		t.Fatalf("result text = %q", got)
	}
	wantMethods := "initialize,notifications/initialized,tools/list,tools/list,tools/call"
	if got := strings.Join(methods, ","); got != wantMethods {
		t.Fatalf("methods = %q, want %q", got, wantMethods)
	}
}
