// Package memorybank gives the agent a memory that outlives one conversation:
// Vertex AI Agent Engine Memory Bank, scoped per user.
//
// It implements google.golang.org/adk/v2/memory.Service, so the ADK graph gets
// preload_memory and load_memory for free, and the voice session recalls the
// same facts before the call starts.
//
// The ADK's own Memory Bank client feeds GenerateMemories from a *Vertex*
// session resource, which assumes sessions live in Agent Engine. Ours live in
// Firestore (internal/fsession), so this one uses DirectContentsSource: the
// conversation's turns are sent verbatim, and Memory Bank's extractor decides
// which facts are worth keeping ("teaches English online", "prefers short
// answers"). Consolidation and deduplication happen server-side.
package memorybank

import (
	"context"
	"fmt"
	"strings"
	"time"

	aiplatform "cloud.google.com/go/aiplatform/apiv1beta1"
	"cloud.google.com/go/aiplatform/apiv1beta1/aiplatformpb"
	"google.golang.org/api/option"
	"google.golang.org/genai"

	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/session"
)

// Service is a memory.Service backed by one Agent Engine's Memory Bank.
type Service struct {
	client *aiplatform.MemoryBankClient
	parent string // projects/{p}/locations/{l}/reasoningEngines/{id}
	// MaxEvents bounds how much of a conversation is sent for extraction.
	MaxEvents int
	// TopK is how many facts a search returns (Memory Bank defaults to 3).
	TopK int32
}

var _ memory.Service = (*Service)(nil)

// New opens a Memory Bank client against an existing Agent Engine instance.
// Application Default Credentials; on Cloud Run that is the runtime service
// account, which needs roles/aiplatform.user.
func New(ctx context.Context, projectID, location, engineID string) (*Service, error) {
	if projectID == "" || engineID == "" {
		return nil, fmt.Errorf("memorybank: project id and agent engine id are required")
	}
	if location == "" {
		location = "us-central1"
	}
	c, err := aiplatform.NewMemoryBankClient(ctx, option.WithEndpoint(location+"-aiplatform.googleapis.com:443"))
	if err != nil {
		return nil, fmt.Errorf("memorybank: client: %w", err)
	}
	return &Service{
		client:    c,
		parent:    fmt.Sprintf("projects/%s/locations/%s/reasoningEngines/%s", projectID, location, engineID),
		MaxEvents: 40,
		TopK:      5,
	}, nil
}

func (s *Service) Close() error { return s.client.Close() }

// Parent is the Agent Engine resource this memory lives in.
func (s *Service) Parent() string { return s.parent }

// AddSessionToMemory hands a finished conversation to Memory Bank, which
// extracts the durable facts. Called once per turn is fine: the service
// consolidates rather than duplicating.
func (s *Service) AddSessionToMemory(ctx context.Context, sess session.Session) error {
	events := contentsOf(sess, s.MaxEvents)
	if len(events) == 0 {
		return nil // nothing said yet; nothing to remember
	}
	_, err := s.client.GenerateMemories(ctx, &aiplatformpb.GenerateMemoriesRequest{
		Parent: s.parent,
		Source: &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_{
			DirectContentsSource: &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource{Events: events},
		},
		Scope: scopeFor(sess.UserID()),
	})
	if err != nil {
		return fmt.Errorf("memorybank: generate memories: %w", err)
	}
	// The operation finishes server-side; extraction takes seconds and the
	// caller should not wait on a chat turn for it.
	return nil
}

// SearchMemory returns the facts most relevant to a query for one user.
func (s *Service) SearchMemory(ctx context.Context, req *memory.SearchRequest) (*memory.SearchResponse, error) {
	if req == nil || strings.TrimSpace(req.Query) == "" {
		return &memory.SearchResponse{Memories: []memory.Entry{}}, nil
	}
	res, err := s.client.RetrieveMemories(ctx, &aiplatformpb.RetrieveMemoriesRequest{
		Parent: s.parent,
		Scope:  scopeFor(req.UserID),
		RetrievalParams: &aiplatformpb.RetrieveMemoriesRequest_SimilaritySearchParams_{
			SimilaritySearchParams: &aiplatformpb.RetrieveMemoriesRequest_SimilaritySearchParams{
				SearchQuery: req.Query,
				TopK:        s.TopK,
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("memorybank: retrieve memories: %w", err)
	}
	out := &memory.SearchResponse{Memories: make([]memory.Entry, 0, len(res.GetRetrievedMemories()))}
	for _, m := range res.GetRetrievedMemories() {
		mem := m.GetMemory()
		if mem == nil || strings.TrimSpace(mem.GetFact()) == "" {
			continue
		}
		entry := memory.Entry{
			ID:      mem.GetName(),
			Content: genai.NewContentFromText(mem.GetFact(), genai.RoleUser),
			Author:  "user",
		}
		if ts := mem.GetUpdateTime(); ts != nil {
			entry.Timestamp = ts.AsTime()
		}
		out.Memories = append(out.Memories, entry)
	}
	return out, nil
}

// Recall is the plain-text form the voice session needs: the facts to put in
// front of the model before the first word is spoken.
func (s *Service) Recall(ctx context.Context, userID, query string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	res, err := s.SearchMemory(ctx, &memory.SearchRequest{Query: query, UserID: userID})
	if err != nil {
		return nil, err
	}
	facts := make([]string, 0, len(res.Memories))
	for _, m := range res.Memories {
		if m.Content == nil {
			continue
		}
		for _, p := range m.Content.Parts {
			if t := strings.TrimSpace(p.Text); t != "" {
				facts = append(facts, t)
			}
		}
	}
	return facts, nil
}

// contentsOf takes the last n non-partial turns of a session as extraction
// input. Only text is sent: images and tool payloads carry no durable fact
// about the person, and they would inflate every request.
func contentsOf(sess session.Session, n int) []*aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_Event {
	if sess == nil {
		return nil
	}
	var contents []*aiplatformpb.Content
	for ev := range sess.Events().All() {
		if ev == nil || ev.Partial || ev.Content == nil {
			continue
		}
		role := ev.Content.Role
		if role != "user" && role != "model" {
			continue
		}
		var text []string
		for _, p := range ev.Content.Parts {
			if t := strings.TrimSpace(p.Text); t != "" {
				text = append(text, t)
			}
		}
		if len(text) == 0 {
			continue
		}
		contents = append(contents, &aiplatformpb.Content{
			Role:  role,
			Parts: []*aiplatformpb.Part{{Data: &aiplatformpb.Part_Text{Text: strings.Join(text, "\n")}}},
		})
	}
	if n > 0 && len(contents) > n {
		contents = contents[len(contents)-n:]
	}
	events := make([]*aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_Event, 0, len(contents))
	for _, c := range contents {
		events = append(events, &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_Event{Content: c})
	}
	return events
}

// scopeFor keys memories to one person. Every read and write carries it, so
// one user's preferences can never surface in another's conversation.
func scopeFor(userID string) map[string]string {
	if userID == "" {
		userID = "unknown"
	}
	return map[string]string{"user_id": userID}
}

// Transcript is a conversation the voice session hands over when the call
// ends: the Live API keeps its turns outside the ADK session, so they reach
// Memory Bank through this instead of through a session.Session.
type Transcript struct {
	UserID string
	Turns  []Turn
}

// Turn is one spoken exchange. Role is "user" or "model".
type Turn struct {
	Role string
	Text string
}

// AddTranscript sends spoken turns for extraction, on the same user scope the
// typed chat writes to — one memory of the person, whichever way they talked.
func (s *Service) AddTranscript(ctx context.Context, t Transcript) error {
	events := make([]*aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_Event, 0, len(t.Turns))
	for _, turn := range t.Turns {
		text := strings.TrimSpace(turn.Text)
		role := turn.Role
		if role != "user" && role != "model" {
			role = "user"
		}
		if text == "" {
			continue
		}
		events = append(events, &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_Event{
			Content: &aiplatformpb.Content{Role: role, Parts: []*aiplatformpb.Part{{Data: &aiplatformpb.Part_Text{Text: text}}}},
		})
	}
	if len(events) == 0 {
		return nil
	}
	if len(events) > s.MaxEvents && s.MaxEvents > 0 {
		events = events[len(events)-s.MaxEvents:]
	}
	_, err := s.client.GenerateMemories(ctx, &aiplatformpb.GenerateMemoriesRequest{
		Parent: s.parent,
		Source: &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource_{
			DirectContentsSource: &aiplatformpb.GenerateMemoriesRequest_DirectContentsSource{Events: events},
		},
		Scope: scopeFor(t.UserID),
	})
	if err != nil {
		return fmt.Errorf("memorybank: generate memories from transcript: %w", err)
	}
	return nil
}
