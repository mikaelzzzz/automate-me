package httpapi

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"google.golang.org/genai"

	"automate-me/app/internal/agents"
)

// Live voice: the browser holds the microphone and talks straight to the
// Gemini Live API over a WebSocket, so the audio never round-trips through us.
// What it cannot do is act — every function the model calls comes back here,
// to /app/api/live/tool, and runs the same code the ADK graph runs.
//
//	mic ──▶ Live API ──tool call──▶ /app/api/live/tool ──▶ Value Engine · store
//	                                                        Routes · Weather
//	    ◀── speech ◀── tool result ◀───────────────────────┘
//
// The API key stays on the server: the browser gets a single-use ephemeral
// token instead.

// LiveDeps is what the live endpoints need beyond the dashboard handler.
type LiveDeps struct {
	// Tools is the registry shared with the agent graph. Nil disables voice.
	Tools map[string]agents.LiveTool
	// Client mints ephemeral tokens; nil disables voice.
	Client *genai.Client
	Model  string
	// SystemInstruction is authored here, never by the browser.
	SystemInstruction string
	// Voice is the prebuilt voice name for the spoken reply.
	Voice string
}

type liveSessionResponse struct {
	Available bool `json:"available"`
	// Token is an ephemeral, single-use credential for the Live WebSocket.
	Token             string           `json:"token,omitempty"`
	Model             string           `json:"model,omitempty"`
	Voice             string           `json:"voice,omitempty"`
	SystemInstruction string           `json:"system_instruction,omitempty"`
	Tools             []map[string]any `json:"tools,omitempty"`
	Reason            string           `json:"reason,omitempty"`
}

func (h *Handler) liveSession(w http.ResponseWriter, r *http.Request) {
	if h.Live.Client == nil || len(h.Live.Tools) == 0 {
		writeJSON(w, http.StatusOK, liveSessionResponse{Available: false, Reason: "GOOGLE_API_KEY is not configured on this server"})
		return
	}
	// Single use, and the session dies after 30 minutes even if the tab stays
	// open — a leaked token is worth one short conversation, nothing more.
	uses := int32(1)
	tok, err := h.Live.Client.AuthTokens.Create(r.Context(), &genai.CreateAuthTokenConfig{
		Uses:                 &uses,
		ExpireTime:           time.Now().Add(30 * time.Minute),
		NewSessionExpireTime: time.Now().Add(2 * time.Minute),
		// Pin the model so the token cannot be spent on anything else.
		LiveConnectConstraints: &genai.LiveConnectConstraints{Model: h.Live.Model},
		LockAdditionalFields:   []string{},
	})
	if err != nil {
		slog.Error("mint live token", "err", err)
		writeJSON(w, http.StatusOK, liveSessionResponse{Available: false, Reason: "could not mint a live token: " + err.Error()})
		return
	}

	decls := make([]map[string]any, 0, len(h.Live.Tools))
	for _, name := range agents.LiveToolOrder {
		if t, ok := h.Live.Tools[name]; ok {
			decls = append(decls, t.Declaration)
		}
	}
	writeJSON(w, http.StatusOK, liveSessionResponse{
		Available: true, Token: tok.Name, Model: h.Live.Model, Voice: h.Live.Voice,
		SystemInstruction: h.Live.SystemInstruction, Tools: decls,
	})
}

type liveToolRequest struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args"`
}

type liveToolResponse struct {
	Name   string `json:"name"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// liveTool runs one function the voice model asked for. The name is matched
// against the registry, never used to build a call dynamically, so the voice
// session can only reach the tools the graph itself exposes — and none of them
// signs a payment mandate.
func (h *Handler) liveTool(w http.ResponseWriter, r *http.Request) {
	var req liveToolRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, liveToolResponse{Error: err.Error()})
		return
	}
	t, ok := h.Live.Tools[req.Name]
	if !ok {
		writeJSON(w, http.StatusNotFound, liveToolResponse{Name: req.Name, Error: "unknown tool"})
		return
	}
	started := time.Now()
	res, err := t.Invoke(r.Context(), h.UserID(r), req.Args)
	if err != nil {
		slog.Warn("live tool failed", "tool", req.Name, "err", err)
		// The model needs to hear the failure, not a broken socket.
		writeJSON(w, http.StatusOK, liveToolResponse{Name: req.Name, Error: err.Error()})
		return
	}
	slog.Info("live tool", "tool", req.Name, "took", time.Since(started).Round(time.Millisecond))
	writeJSON(w, http.StatusOK, liveToolResponse{Name: req.Name, Result: res})
}
