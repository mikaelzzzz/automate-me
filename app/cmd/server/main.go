// The automate-me service: agent graph behind adkrest (/api), SPA-facing JSON
// API (/app/api), Trusted Surface consent endpoint, and the built SPA.
package main

import (
	"cmp"
	"context"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo; the briefing needs America/Sao_Paulo

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"automate-me/app/internal/agents"
	"automate-me/app/internal/briefing"
	"automate-me/app/internal/fsession"
	"automate-me/app/internal/httpapi"
	"automate-me/app/internal/memorybank"
	"automate-me/app/internal/proposer"
	"automate-me/app/internal/shopping"
	"automate-me/app/internal/store"
	"automate-me/app/internal/trusted"
)

func main() {
	ctx := context.Background()

	st := store.NewMemory()
	if cmp.Or(os.Getenv("DEMO_MODE"), "seed") == "seed" {
		if err := store.SeedDemo(ctx, st, time.Now()); err != nil {
			log.Fatalf("seed demo data: %v", err)
		}
		// The demo starts where an agent has already been: the same matcher
		// the propose_automations tool runs, over the seeded routines.
		props, err := proposer.Propose(ctx, st, store.DemoUserID, "")
		if err != nil {
			log.Fatalf("seed proposals: %v", err)
		}
		slog.Info("demo mode: seeded in-memory store", "user", store.DemoUserID, "proposals", len(props))
	}

	merchantURL := cmp.Or(os.Getenv("MERCHANT_URL"), "http://localhost:8081")
	merchant := shopping.NewMerchantClient(merchantURL)
	if os.Getenv("MERCHANT_AUTH") == "idtoken" {
		// Cloud Run: the merchant is private; authenticate as this service.
		var err error
		if merchant, err = shopping.NewAuthenticatedMerchantClient(ctx, merchantURL); err != nil {
			log.Fatalf("merchant client: %v", err)
		}
	}
	surface := trusted.NewSurface(st, merchant)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	// Daily Briefing: Maps Platform key (Routes + Weather). Absent → the
	// briefing endpoints and the day_planner report "unavailable".
	var planner *briefing.Builder
	var blocks briefing.BlockWriter
	var events briefing.EventSource
	if key := os.Getenv("MAPS_API_KEY"); key != "" {
		loc, err := time.LoadLocation("America/Sao_Paulo")
		if err != nil {
			log.Fatalf("load timezone: %v", err)
		}
		planner = briefing.NewBuilder(briefing.NewMapsClient(key), loc)
		// No calendar connected: seeded appointments, simulated blocks.
		blocks, events = briefing.SimulatedBlocks{}, briefing.DemoSource{Loc: loc}
		mode := "simulated"
		if calID := os.Getenv("CALENDAR_ID"); calID != "" {
			if g, err := briefing.NewGoogleCalendar(ctx, calID, loc, os.Getenv("HOME_ADDRESS")); err != nil {
				slog.Warn("google calendar unavailable; seeded appointments, simulated blocks", "err", err)
			} else {
				blocks, events = g, g
				mode = "google:" + strings.Join(g.Calendars(), ",")
			}
		}
		slog.Info("daily briefing enabled", "tz", loc.String(), "flood_points", len(briefing.HistoricFloodPoints()), "calendar", mode)
	} else {
		slog.Warn("MAPS_API_KEY not set; daily briefing disabled")
	}

	// Memory: Vertex AI Agent Engine Memory Bank, one scope per user. What the
	// agent learns in a typed chat is what the voice session recalls, and the
	// other way round. Absent MEMORY_ENGINE, the agent starts every
	// conversation as a stranger.
	var mem *memorybank.Service
	if engine := os.Getenv("MEMORY_ENGINE"); engine != "" {
		project := cmp.Or(os.Getenv("MEMORY_PROJECT"), os.Getenv("FIRESTORE_PROJECT"))
		location := cmp.Or(os.Getenv("MEMORY_LOCATION"), "us-central1")
		m, err := memorybank.New(ctx, project, location, engine)
		if err != nil {
			log.Fatalf("memory bank: %v", err)
		}
		defer m.Close()
		mem = m
		slog.Info("memory bank enabled", "engine", m.Parent())
	} else {
		slog.Warn("MEMORY_ENGINE not set; the agent remembers nothing between sessions")
	}

	// Sessions: Firestore when configured, so a conversation survives the
	// revision that hosted it; in-memory otherwise.
	sessions := session.InMemoryService()
	sessionStore := "memory"
	if project := os.Getenv("FIRESTORE_PROJECT"); project != "" {
		fs, err := fsession.New(ctx, project, os.Getenv("FIRESTORE_DATABASE"), os.Getenv("FIRESTORE_PREFIX"))
		if err != nil {
			log.Fatalf("firestore sessions: %v", err)
		}
		defer fs.Close()
		sessions = fs
		sessionStore = "firestore:" + project
	}
	slog.Info("agent sessions", "store", sessionStore)

	// Voice: the browser streams audio straight to the Gemini Live API and
	// posts the model's function calls back to us, where they run the same
	// tools the ADK graph runs.
	deps := agents.Deps{
		Store:    st,
		UserID:   func(agent.Context) string { return store.DemoUserID },
		Briefing: planner,
		Blocks:   blocks,
		Events:   events,
		Memory:   mem,
		Sessions: sessions,
	}

	// Build the graph first: the voice tools delegate into it, so it has to
	// exist before the live tool set is frozen.
	// A typed-nil *memorybank.Service would satisfy memory.Service and then
	// fail on every call; hand over an interface that is genuinely nil.
	var memSvc memory.Service
	if mem != nil {
		memSvc = mem
	}
	consult, err := mountChat(ctx, mux, deps, sessions, memSvc)
	if err != nil {
		// Dashboard + consent must come up even without a model key.
		slog.Warn("chat API disabled (no model?)", "err", err)
	} else {
		deps.Consult = consult
	}

	// Voice: the browser streams audio straight to the Gemini Live API and
	// posts the model's function calls back to us, where they run the same
	// tools the ADK graph runs. The Live model is the only conversational one
	// available (3.1); everything that needs judgement is handed to the graph
	// on gemini-3.5-flash through consult_specialist.
	live := httpapi.LiveDeps{
		Tools:             deps.LiveTools(),
		Model:             cmp.Or(os.Getenv("LIVE_MODEL"), "gemini-3.1-flash-live-preview"),
		Voice:             cmp.Or(os.Getenv("LIVE_VOICE"), "Zephyr"),
		ReasoningModel:    graphModel(),
		SystemInstruction: agents.LiveSystemInstruction,
		Memory:            mem,
	}
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
		gc, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
		if err != nil {
			slog.Warn("voice disabled: genai client", "err", err)
		} else {
			live.Client = gc
			slog.Info("live voice enabled", "voice_model", live.Model, "reasoning_model", live.ReasoningModel,
				"voice", live.Voice, "tools", len(live.Tools), "delegates_to_graph", deps.Consult != nil)
		}
	} else {
		slog.Warn("GOOGLE_API_KEY not set; live voice disabled")
	}

	api := &httpapi.Handler{
		Store:    st,
		Trusted:  surface,
		Briefing: planner,
		Blocks:   blocks,
		Events:   events,
		Live:     live,
		UserID:   func(*http.Request) string { return store.DemoUserID },
	}
	api.Register(mux)

	if dist := os.Getenv("WEB_DIST"); dist != "" {
		if _, err := os.Stat(dist); err == nil {
			mux.Handle("/", spaHandler(dist))
			slog.Info("serving SPA", "dir", dist)
		} else {
			slog.Warn("WEB_DIST not found; API only", "dir", dist)
		}
	}

	port := cmp.Or(os.Getenv("PORT"), "8080")
	slog.Info("automate-me listening", "port", port, "merchant", merchantURL)
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadTimeout: 30 * time.Second, WriteTimeout: 300 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

func graphModel() string { return cmp.Or(os.Getenv("GEMINI_MODEL"), "gemini-3.5-flash") }

// mountChat builds the agent graph, serves it over adkrest for the typed chat,
// and returns a closure that runs the same graph one question at a time — the
// voice session's route into Gemini 3.5 Flash.
func mountChat(ctx context.Context, mux *http.ServeMux, d agents.Deps, sessions session.Service, mem memory.Service) (
	func(context.Context, string, string) (agents.Consultation, error), error,
) {
	model := graphModel()
	llm, err := gemini.NewModel(ctx, model, &genai.ClientConfig{})
	if err != nil {
		return nil, err
	}
	root, err := agents.New(llm, d)
	if err != nil {
		return nil, err
	}
	srv, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     agent.NewSingleLoader(root),
		SessionService:  sessions,
		MemoryService:   mem,
		SSEWriteTimeout: 120 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	mux.Handle("/api/", http.StripPrefix("/api", srv))

	r, err := runner.New(runner.Config{
		AppName:           "automate_me_live",
		Agent:             root,
		SessionService:    sessions,
		MemoryService:     mem,
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, err
	}

	consult := func(ctx context.Context, userID, question string) (agents.Consultation, error) {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		out := agents.Consultation{Model: model}
		seen := map[string]bool{}
		var answer strings.Builder
		// One session per user keeps the graph's memory of the call.
		for ev, err := range r.Run(ctx, userID, "live-"+userID,
			genai.NewContentFromText(question, genai.RoleUser),
			agent.RunConfig{StreamingMode: agent.StreamingModeNone}) {
			if err != nil {
				return out, err
			}
			if ev.Partial || ev.Content == nil {
				continue
			}
			if ev.Author != "" && ev.Author != "user" && !seen["a:"+ev.Author] {
				seen["a:"+ev.Author] = true
				out.Handled = append(out.Handled, ev.Author)
			}
			for _, p := range ev.Content.Parts {
				if p.FunctionCall != nil && !seen["t:"+p.FunctionCall.Name] {
					seen["t:"+p.FunctionCall.Name] = true
					out.ToolsRun = append(out.ToolsRun, p.FunctionCall.Name)
				}
				if ev.IsFinalResponse() && p.Text != "" {
					answer.WriteString(p.Text)
				}
			}
		}
		out.Answer = strings.TrimSpace(answer.String())
		if out.Answer == "" {
			return out, fmt.Errorf("the specialist graph returned nothing")
		}
		return out, nil
	}
	return consult, nil
}

// spaHandler serves the built SPA with an index.html fallback for client-side
// routes.
func spaHandler(dist string) http.Handler {
	fs := http.FileServer(http.Dir(dist))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := dist + r.URL.Path
		if r.URL.Path != "/" {
			if _, err := os.Stat(path); err != nil {
				http.ServeFile(w, r, dist+"/index.html")
				return
			}
		}
		fs.ServeHTTP(w, r)
	})
}
