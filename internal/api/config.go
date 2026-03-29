package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bryanneva/ponko/internal/channel"
	"github.com/bryanneva/ponko/internal/slack"
)

type channelListItem struct {
	ChannelID        string   `json:"channelId"`
	ChannelName      string   `json:"channelName"`
	SystemPrompt     string   `json:"systemPrompt"`
	RespondMode      string   `json:"respondMode"`
	CreatedAt        string   `json:"createdAt"`
	UpdatedAt        string   `json:"updatedAt"`
	ToolAllowlist    []string `json:"toolAllowlist"`
	ApprovalRequired bool     `json:"approvalRequired"`
}

func handleListChannels(pool *pgxpool.Pool, slackClient *slack.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		configs, err := channel.ListAll(r.Context(), pool)
		if err != nil {
			slog.Error("failed to list channel configs", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list channel configs")
			return
		}

		items := make([]channelListItem, len(configs))
		for i, c := range configs {
			items[i] = channelListItem{
				ChannelID:        c.ChannelID,
				SystemPrompt:     c.SystemPrompt,
				RespondMode:      c.RespondMode,
				ToolAllowlist:    c.ToolAllowlist,
				ApprovalRequired: c.ApprovalRequired,
				CreatedAt:        c.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
				UpdatedAt:        c.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
			}
			name, nameErr := slackClient.GetChannelName(r.Context(), c.ChannelID)
			if nameErr == nil {
				items[i].ChannelName = name
			} else {
				slog.Warn("failed to get channel name", "channel_id", c.ChannelID, "error", nameErr)
			}
		}
		writeJSON(w, http.StatusOK, items)
	}
}

type channelConfigRequest struct {
	SystemPrompt     string   `json:"system_prompt"`
	RespondMode      string   `json:"respond_mode"`
	ToolAllowlist    []string `json:"tool_allowlist"`
	ApprovalRequired bool     `json:"approval_required"`
}

type channelConfigResponse struct {
	ChannelID        string   `json:"channel_id"`
	ChannelName      string   `json:"channel_name,omitempty"`
	SystemPrompt     string   `json:"system_prompt"`
	RespondMode      string   `json:"respond_mode"`
	ToolAllowlist    []string `json:"tool_allowlist"`
	ApprovalRequired bool     `json:"approval_required"`
}

func handleGetChannelConfig(pool *pgxpool.Pool, slackClient *slack.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := r.PathValue("id")
		if channelID == "" {
			writeError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		resp := channelConfigResponse{ChannelID: channelID}
		if name, nameErr := slackClient.GetChannelName(r.Context(), channelID); nameErr == nil {
			resp.ChannelName = name
		}

		cfg, err := channel.GetConfig(r.Context(), pool, channelID)
		if err != nil {
			slog.Error("failed to get channel config", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to get channel config")
			return
		}

		if cfg != nil {
			resp.SystemPrompt = cfg.SystemPrompt
			resp.RespondMode = cfg.RespondMode
			resp.ToolAllowlist = cfg.ToolAllowlist
			resp.ApprovalRequired = cfg.ApprovalRequired
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func handlePutChannelConfig(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := r.PathValue("id")
		if channelID == "" {
			writeError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		var req channelConfigRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}

		if req.SystemPrompt == "" {
			err := channel.DeleteConfig(r.Context(), pool, channelID)
			if err != nil {
				slog.Error("failed to delete channel config", "error", err)
				writeError(w, http.StatusInternalServerError, "failed to delete channel config")
				return
			}
			writeJSON(w, http.StatusOK, channelConfigResponse{ChannelID: channelID})
			return
		}

		if req.RespondMode != "" && req.RespondMode != channel.RespondModeMentionOnly && req.RespondMode != channel.RespondModeAllMessages {
			writeError(w, http.StatusBadRequest, "invalid respond_mode: must be 'mention_only' or 'all_messages'")
			return
		}

		cfg := &channel.Config{
			ChannelID:        channelID,
			SystemPrompt:     req.SystemPrompt,
			RespondMode:      req.RespondMode,
			ToolAllowlist:    req.ToolAllowlist,
			ApprovalRequired: req.ApprovalRequired,
		}
		if err := channel.UpsertConfig(r.Context(), pool, cfg); err != nil {
			slog.Error("failed to upsert channel config", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to save channel config")
			return
		}

		writeJSON(w, http.StatusOK, channelConfigResponse{
			ChannelID:        cfg.ChannelID,
			SystemPrompt:     cfg.SystemPrompt,
			RespondMode:      cfg.RespondMode,
			ToolAllowlist:    cfg.ToolAllowlist,
			ApprovalRequired: cfg.ApprovalRequired,
		})
	}
}

func handleListTools(toolNames []string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string][]string{"tools": toolNames})
	}
}

