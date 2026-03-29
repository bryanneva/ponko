package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/bryanneva/ponko/internal/llm"
)

const (
	mcpProtocolVersion = "2025-03-26"
	clientName         = "ponko"
	clientVersion      = "1.0.0"
)

type Client struct {
	HTTP        *http.Client
	BaseURL     string
	BearerToken string
	sessionID   string

	mu            sync.Mutex
	nextID        int
	initialized   bool
	initAttempted bool
}

type jsonRPCRequest struct {
	Params  any    `json:"params,omitempty"`
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	ID      int    `json:"id"`
}

type jsonRPCNotification struct {
	Params  any    `json:"params,omitempty"`
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
}

type initializeParams struct {
	Capabilities    struct{}   `json:"capabilities"`
	ClientInfo      clientInfo `json:"clientInfo"`
	ProtocolVersion string     `json:"protocolVersion"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type initializeResponse struct {
	Error  *jsonRPCError   `json:"error"`
	Result json.RawMessage `json:"result"`
	ID     int             `json:"id"`
}

type toolCallParams struct {
	Arguments map[string]any `json:"arguments"`
	Name      string         `json:"name"`
}

type jsonRPCError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}

type contentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallResult struct {
	Content []contentItem `json:"content"`
}

type toolCallResponse struct {
	Error  *jsonRPCError   `json:"error"`
	Result *toolCallResult `json:"result"`
	ID     int             `json:"id"`
}

type toolsListResponse struct {
	Error  *jsonRPCError    `json:"error"`
	Result *toolsListResult `json:"result"`
	ID     int              `json:"id"`
}

type toolsListResult struct {
	Tools []mcpTool `json:"tools"`
}

type mcpTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// nextRequestID must be called with c.mu held.
func (c *Client) nextRequestID() int {
	c.nextID++
	return c.nextID
}

func (c *Client) newPostRequest(ctx context.Context, body []byte) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.BearerToken)
	}
	return req, nil
}

func (c *Client) ensureInitialized(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.initialized || c.initAttempted {
		return nil
	}
	return c.initialize(ctx)
}

// initialize performs the MCP protocol handshake. Must be called with c.mu held.
func (c *Client) initialize(ctx context.Context) error {
	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      c.nextRequestID(),
		Method:  "initialize",
		Params: initializeParams{
			ProtocolVersion: mcpProtocolVersion,
			Capabilities:    struct{}{},
			ClientInfo:      clientInfo{Name: clientName, Version: clientVersion},
		},
	}

	body, marshalErr := json.Marshal(rpcReq)
	if marshalErr != nil {
		return fmt.Errorf("marshaling initialize: %w", marshalErr)
	}

	req, reqErr := c.newPostRequest(ctx, body)
	if reqErr != nil {
		return fmt.Errorf("creating initialize request: %w", reqErr)
	}

	resp, doErr := c.HTTP.Do(req)
	if doErr != nil {
		return fmt.Errorf("sending initialize: %w", doErr)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return fmt.Errorf("reading initialize response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("initialize error (%d): %s", resp.StatusCode, string(respBody))
	}

	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.sessionID = sid
	}

	var rpcResp initializeResponse
	if unmarshalErr := json.Unmarshal(respBody, &rpcResp); unmarshalErr != nil {
		return fmt.Errorf("unmarshaling initialize response: %w", unmarshalErr)
	}
	if rpcResp.Error != nil {
		return fmt.Errorf("initialize JSON-RPC error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if notifErr := c.sendNotification(ctx, "notifications/initialized"); notifErr != nil {
		return fmt.Errorf("sending initialized notification: %w", notifErr)
	}

	c.initialized = true
	return nil
}

// Initialize performs the MCP protocol handshake. Satisfies the Server interface.
// Marks init as attempted so that ensureInitialized won't retry on failure,
// allowing ListTools/CallTool to proceed for servers that don't support init.
func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.initAttempted = true
	if c.initialized {
		return nil
	}
	return c.initialize(ctx)
}

func (c *Client) sendNotification(ctx context.Context, method string) error {
	notif := jsonRPCNotification{JSONRPC: "2.0", Method: method}
	body, marshalErr := json.Marshal(notif)
	if marshalErr != nil {
		return fmt.Errorf("marshaling notification: %w", marshalErr)
	}

	req, reqErr := c.newPostRequest(ctx, body)
	if reqErr != nil {
		return reqErr
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}

	resp, doErr := c.HTTP.Do(req)
	if doErr != nil {
		return fmt.Errorf("sending notification: %w", doErr)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("notification rejected (%d): %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) doRequest(ctx context.Context, method string, params any) ([]byte, int, error) {
	c.mu.Lock()
	id := c.nextRequestID()
	sessionID := c.sessionID
	c.mu.Unlock()

	rpcReq := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, marshalErr := json.Marshal(rpcReq)
	if marshalErr != nil {
		return nil, 0, fmt.Errorf("marshaling request: %w", marshalErr)
	}

	req, reqErr := c.newPostRequest(ctx, body)
	if reqErr != nil {
		return nil, 0, fmt.Errorf("creating request: %w", reqErr)
	}
	if sessionID != "" {
		req.Header.Set("Mcp-Session-Id", sessionID)
	}

	resp, doErr := c.HTTP.Do(req)
	if doErr != nil {
		return nil, 0, fmt.Errorf("sending request: %w", doErr)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, 0, fmt.Errorf("reading response: %w", readErr)
	}

	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		parsed, sseErr := parseSSEResponse(respBody)
		if sseErr != nil {
			return nil, 0, fmt.Errorf("parsing SSE response: %w", sseErr)
		}
		respBody = parsed
	}

	return respBody, resp.StatusCode, nil
}

func (c *Client) sendRequest(ctx context.Context, method string, params any) ([]byte, error) {
	if err := c.ensureInitialized(ctx); err != nil {
		return nil, fmt.Errorf("initialization failed: %w", err)
	}

	respBody, statusCode, err := c.doRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	if statusCode == http.StatusNotFound {
		c.mu.Lock()
		c.initialized = false
		c.initAttempted = false
		c.sessionID = ""
		c.mu.Unlock()

		if reinitErr := c.ensureInitialized(ctx); reinitErr != nil {
			return nil, fmt.Errorf("re-initialization failed: %w", reinitErr)
		}
		respBody, statusCode, err = c.doRequest(ctx, method, params)
		if err != nil {
			return nil, err
		}
	}

	if statusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP server error (%d): %s", statusCode, string(respBody))
	}

	return respBody, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (string, error) {
	respBody, err := c.sendRequest(ctx, "tools/call", toolCallParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", err
	}

	var rpcResp toolCallResponse
	if unmarshalErr := json.Unmarshal(respBody, &rpcResp); unmarshalErr != nil {
		return "", fmt.Errorf("unmarshaling response: %w", unmarshalErr)
	}

	if rpcResp.Error != nil {
		return "", fmt.Errorf("JSON-RPC error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if rpcResp.Result == nil || len(rpcResp.Result.Content) == 0 {
		return "", fmt.Errorf("empty result from MCP server")
	}

	return rpcResp.Result.Content[0].Text, nil
}

func (c *Client) ListTools(ctx context.Context) ([]llm.Tool, error) {
	respBody, err := c.sendRequest(ctx, "tools/list", nil)
	if err != nil {
		return nil, err
	}

	var rpcResp toolsListResponse
	if unmarshalErr := json.Unmarshal(respBody, &rpcResp); unmarshalErr != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", unmarshalErr)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("JSON-RPC error (%d): %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	if rpcResp.Result == nil {
		return nil, fmt.Errorf("empty result from MCP server")
	}

	tools := make([]llm.Tool, len(rpcResp.Result.Tools))
	for i, t := range rpcResp.Result.Tools {
		tools[i] = llm.Tool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		}
	}

	return tools, nil
}
