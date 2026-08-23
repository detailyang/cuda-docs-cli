package nvidia

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type TokenSource interface {
	Token(context.Context) (string, error)
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

type InputSchema struct {
	Type       string              `json:"type,omitempty"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
	raw        json.RawMessage
}

func (schema *InputSchema) UnmarshalJSON(data []byte) error {
	type plainSchema InputSchema
	var decoded plainSchema
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*schema = InputSchema(decoded)
	schema.raw = append(schema.raw[:0], data...)
	return nil
}

func (schema InputSchema) MarshalJSON() ([]byte, error) {
	if len(schema.raw) > 0 {
		return schema.raw, nil
	}
	type plainSchema InputSchema
	return json.Marshal(plainSchema(schema))
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description,omitempty"`
	InputSchema InputSchema `json:"inputSchema"`
	raw         json.RawMessage
}

func (tool *Tool) UnmarshalJSON(data []byte) error {
	type plainTool Tool
	var decoded plainTool
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*tool = Tool(decoded)
	tool.raw = append(tool.raw[:0], data...)
	return nil
}

func (tool Tool) MarshalJSON() ([]byte, error) {
	if len(tool.raw) > 0 {
		return tool.raw, nil
	}
	type plainTool Tool
	return json.Marshal(plainTool(tool))
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolResult struct {
	Content           []Content      `json:"content,omitempty"`
	StructuredContent map[string]any `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
	raw               json.RawMessage
}

func (result *ToolResult) UnmarshalJSON(data []byte) error {
	type plainResult ToolResult
	var decoded plainResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*result = ToolResult(decoded)
	result.raw = append(result.raw[:0], data...)
	return nil
}

func (result ToolResult) MarshalJSON() ([]byte, error) {
	if len(result.raw) > 0 {
		return result.raw, nil
	}
	type plainResult ToolResult
	return json.Marshal(plainResult(result))
}

type Client struct {
	endpoint   string
	tokens     TokenSource
	httpClient *http.Client

	mu          sync.Mutex
	requestID   int64
	sessionID   string
	initialized bool
}

func NewClient(endpoint string, tokens TokenSource, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{endpoint: endpoint, tokens: tokens, httpClient: httpClient}
}

func ParseResponse(body []byte) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	var messages [][]byte
	if trimmed[0] == '{' {
		messages = append(messages, trimmed)
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(body))
		scanner.Buffer(make([]byte, 64<<10), 8<<20)
		var data []string
		flush := func() {
			if len(data) > 0 {
				messages = append(messages, []byte(strings.Join(data, "\n")))
				data = nil
			}
		}
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				flush()
				continue
			}
			if strings.HasPrefix(line, "data:") {
				data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		flush()
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("scan event stream: %w", err)
		}
	}
	for index := len(messages) - 1; index >= 0; index-- {
		var envelope struct {
			Result json.RawMessage `json:"result"`
			Error  *rpcError       `json:"error"`
		}
		if err := json.Unmarshal(messages[index], &envelope); err != nil {
			return nil, fmt.Errorf("decode server response: %w", err)
		}
		if envelope.Error != nil {
			return nil, envelope.Error
		}
		if envelope.Result != nil {
			return envelope.Result, nil
		}
	}
	return nil, nil
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (problem *rpcError) Error() string {
	return fmt.Sprintf("remote error %d: %s", problem.Code, problem.Message)
}

func ChooseSearchInvocation(tools []Tool, query string) (string, map[string]any, error) {
	preferredFields := []string{"query", "search_phrase", "search_query", "question", "text"}
	for _, tool := range tools {
		searchable := strings.ToLower(tool.Name + " " + tool.Description)
		if !strings.Contains(searchable, "search") && !strings.Contains(searchable, "query") {
			continue
		}
		field := ""
		for _, candidate := range preferredFields {
			if property, ok := tool.InputSchema.Properties[candidate]; ok && property.Type == "string" {
				field = candidate
				break
			}
		}
		if field == "" {
			for name, property := range tool.InputSchema.Properties {
				if property.Type == "string" {
					field = name
					break
				}
			}
		}
		if field == "" || hasOtherRequiredFields(tool.InputSchema.Required, field) {
			continue
		}
		return tool.Name, map[string]any{field: query}, nil
	}
	return "", nil, errors.New("server did not advertise a compatible documentation search tool; run: cuda-docs tools --json")
}

func hasOtherRequiredFields(required []string, selected string) bool {
	for _, field := range required {
		if field != selected {
			return true
		}
	}
	return false
}

func (client *Client) ListTools(ctx context.Context) ([]Tool, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.initialize(ctx); err != nil {
		return nil, err
	}
	var tools []Tool
	cursor := ""
	for {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		result, err := client.post(ctx, "tools/list", params, false)
		if err != nil {
			return nil, err
		}
		var page struct {
			Tools      []Tool `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(result, &page); err != nil {
			return nil, fmt.Errorf("decode tools list: %w", err)
		}
		tools = append(tools, page.Tools...)
		if page.NextCursor == "" {
			return tools, nil
		}
		cursor = page.NextCursor
	}
}

func (client *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if err := client.initialize(ctx); err != nil {
		return ToolResult{}, err
	}
	result, err := client.post(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, false)
	if err != nil {
		return ToolResult{}, err
	}
	var toolResult ToolResult
	if err := json.Unmarshal(result, &toolResult); err != nil {
		return ToolResult{}, fmt.Errorf("decode tool result: %w", err)
	}
	return toolResult, nil
}

func (client *Client) initialize(ctx context.Context) error {
	if client.initialized {
		return nil
	}
	_, err := client.post(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "cuda-docs-cli", "version": "dev"},
	}, false)
	if err != nil {
		return fmt.Errorf("initialize NVIDIA documentation session: %w", err)
	}
	if _, err := client.post(ctx, "notifications/initialized", nil, true); err != nil {
		return fmt.Errorf("confirm NVIDIA documentation session: %w", err)
	}
	client.initialized = true
	return nil
}

func (client *Client) post(ctx context.Context, method string, params any, notification bool) (json.RawMessage, error) {
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	if !notification {
		client.requestID++
		payload["id"] = client.requestID
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	token, err := client.tokens.Token(ctx)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, client.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if client.sessionID != "" {
		request.Header.Set("Mcp-Session-Id", client.sessionID)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("contact NVIDIA documentation service: %w", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read NVIDIA response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusUnauthorized {
			return nil, errors.New("NVIDIA login is invalid or expired; run: cuda-docs login")
		}
		return nil, fmt.Errorf("NVIDIA service returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if sessionID := response.Header.Get("Mcp-Session-Id"); sessionID != "" {
		client.sessionID = sessionID
	}
	return ParseResponse(body)
}
