package httpapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"google.golang.org/genai"

	"automate-me/app/internal/agents"
	"automate-me/app/internal/memorybank"
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
	// ReasoningModel is the model behind the agent graph the voice delegates
	// to — surfaced so the UI can name the whole chain honestly.
	ReasoningModel string
	// Memory recalls what earlier conversations taught us about this person,
	// and stores what this one does. Nil disables both.
	Memory *memorybank.Service
}

type liveSessionResponse struct {
	Available bool `json:"available"`
	// Token is an ephemeral, single-use credential for the Live WebSocket.
	Token             string           `json:"token,omitempty"`
	Model             string           `json:"model,omitempty"`
	Voice             string           `json:"voice,omitempty"`
	SystemInstruction string           `json:"system_instruction,omitempty"`
	Tools             []map[string]any `json:"tools,omitempty"`
	ReasoningModel    string           `json:"reasoning_model,omitempty"`
	Reason            string           `json:"reason,omitempty"`
	// Memories are the facts injected into this session's instruction, echoed
	// so the UI can show the caller what the agent walked in knowing.
	Memories []string `json:"memories,omitempty"`
}

// recallQuery is what the voice session asks its memory before the call: the
// standing facts that change how the agent should talk to this person.
const recallQuery = "the user's routine, constraints, household, work and how they prefer the agent to talk to them"

// memoryPreamble frames recalled facts for the Live model. Fenced and named,
// so the model treats them as background it already knows rather than as
// something the caller just said.
func memoryPreamble(facts []string) string {
	var b strings.Builder
	b.WriteString("\n\nWHAT YOU ALREADY KNOW ABOUT THIS PERSON (from earlier conversations; never read this list aloud verbatim, and never invent additions):\n")
	for _, f := range facts {
		fmt.Fprintf(&b, "- %s\n", f)
	}
	b.WriteString("Use it to skip questions they have already answered and to match their preferences. If something here contradicts what they say now, believe them, not the note.")
	return b.String()
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

	// Recall before the first word: the Live session has no history of its
	// own, so what the agent remembers has to travel in the instruction.
	instruction := h.Live.SystemInstruction
	var facts []string
	if h.Live.Memory != nil {
		var err error
		if facts, err = h.Live.Memory.Recall(r.Context(), h.UserID(r), recallQuery); err != nil {
			// A cold memory is not a broken call.
			slog.Warn("memory recall failed; starting the call without it", "err", err)
		} else if len(facts) > 0 {
			instruction += memoryPreamble(facts)
			slog.Info("live session recalled memories", "count", len(facts))
		}
	}

	writeJSON(w, http.StatusOK, liveSessionResponse{
		Available: true, Token: tok.Name, Model: h.Live.Model, Voice: h.Live.Voice,
		ReasoningModel: h.Live.ReasoningModel, SystemInstruction: instruction, Tools: decls,
		Memories: facts,
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

type liveRememberRequest struct {
	Turns []struct {
		Role string `json:"role"`
		Text string `json:"text"`
	} `json:"turns"`
}

// liveRemember takes the transcript of a finished call and hands it to Memory
// Bank. The Live API keeps its turns in the browser, so this is the only way
// a spoken conversation reaches the same memory the typed one writes to.
func (h *Handler) liveRemember(w http.ResponseWriter, r *http.Request) {
	if h.Live.Memory == nil {
		writeJSON(w, http.StatusOK, map[string]any{"stored": false, "reason": "memory is not configured"})
		return
	}
	var req liveRememberRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	t := memorybank.Transcript{UserID: h.UserID(r)}
	for _, turn := range req.Turns {
		t.Turns = append(t.Turns, memorybank.Turn{Role: turn.Role, Text: turn.Text})
	}
	if err := h.Live.Memory.AddTranscript(r.Context(), t); err != nil {
		slog.Warn("memory: could not store the call", "err", err)
		writeJSON(w, http.StatusOK, map[string]any{"stored": false, "reason": err.Error()})
		return
	}
	slog.Info("memory: call stored", "turns", len(t.Turns))
	writeJSON(w, http.StatusOK, map[string]any{"stored": true, "turns": len(t.Turns)})
}
