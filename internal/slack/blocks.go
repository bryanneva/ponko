package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Block is the common interface for Slack Block Kit elements.
type Block interface {
	blockType() string
}

// RawBlock wraps pre-serialized JSON so it can be passed as a Block.
type RawBlock json.RawMessage

func (RawBlock) blockType() string { return "raw" }

func (r RawBlock) MarshalJSON() ([]byte, error) {
	return json.RawMessage(r), nil
}

type TextObject struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type SectionBlock struct {
	Text TextObject `json:"text"`
}

func (SectionBlock) blockType() string { return "section" }

func (b SectionBlock) MarshalJSON() ([]byte, error) {
	type alias SectionBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "section",
		alias: alias(b),
	})
}

type ButtonElement struct {
	Text     TextObject `json:"text"`
	ActionID string     `json:"action_id"`
	Value    string     `json:"value,omitempty"`
	Style    string     `json:"style,omitempty"`
}

func (b ButtonElement) MarshalJSON() ([]byte, error) {
	type alias ButtonElement
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "button",
		alias: alias(b),
	})
}

type ActionsBlock struct {
	Elements []ButtonElement `json:"elements"`
}

func (ActionsBlock) blockType() string { return "actions" }

func (b ActionsBlock) MarshalJSON() ([]byte, error) {
	type alias ActionsBlock
	return json.Marshal(struct {
		Type string `json:"type"`
		alias
	}{
		Type:  "actions",
		alias: alias(b),
	})
}

type postBlocksRequest struct {
	Channel  string          `json:"channel"`
	Text     string          `json:"text"`
	ThreadTS string          `json:"thread_ts,omitempty"`
	Blocks   json.RawMessage `json:"blocks,omitempty"`
}

type postBlocksResponse struct {
	Error string `json:"error,omitempty"`
	TS    string `json:"ts"`
	OK    bool   `json:"ok"`
}

func (c *Client) PostBlocks(ctx context.Context, channel, text string, blocks []Block, threadTS string) (string, error) {
	var blocksJSON json.RawMessage
	if len(blocks) > 0 {
		var err error
		blocksJSON, err = json.Marshal(blocks)
		if err != nil {
			return "", fmt.Errorf("marshaling blocks: %w", err)
		}
	}

	reqBody := postBlocksRequest{
		Channel:  channel,
		Text:     text,
		Blocks:   blocksJSON,
		ThreadTS: threadTS,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshaling post blocks request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat.postMessage", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating post blocks request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending post blocks request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading post blocks response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result postBlocksResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshaling post blocks response: %w", err)
	}

	if !result.OK {
		return "", fmt.Errorf("slack API error: %s", result.Error)
	}

	return result.TS, nil
}

type updateMessageRequest struct {
	Channel string          `json:"channel"`
	TS      string          `json:"ts"`
	Text    string          `json:"text"`
	Blocks  json.RawMessage `json:"blocks,omitempty"`
}

func (c *Client) UpdateMessage(ctx context.Context, channel, messageTS, text string, blocks []Block) error {
	var blocksJSON json.RawMessage
	if len(blocks) > 0 {
		var err error
		blocksJSON, err = json.Marshal(blocks)
		if err != nil {
			return fmt.Errorf("marshaling blocks: %w", err)
		}
	}

	reqBody := updateMessageRequest{
		Channel: channel,
		TS:      messageTS,
		Text:    text,
		Blocks:  blocksJSON,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshaling update message request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat.update", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating update message request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending update message request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading update message response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack API HTTP error (%d): %s", resp.StatusCode, string(respBody))
	}

	var result postMessageResponse
	if err = json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("unmarshaling update message response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("slack API error: %s", result.Error)
	}

	return nil
}
