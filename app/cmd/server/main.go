// The automate-me service: agent graph behind adkrest (/api), SPA-facing JSON
// API (/app/api), Trusted Surface consent endpoint, and the built SPA.
package main

import (
	"cmp"
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"
	_ "time/tzdata" // distroless has no zoneinfo; the briefing needs America/Sao_Paulo

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/server/adkrest"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"

	"automate-me/app/internal/agents"
	"automate-me/app/internal/briefing"
	"automate-me/app/internal/httpapi"
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
	if key := os.Getenv("MAPS_API_KEY"); key != "" {
		loc, err := time.LoadLocation("America/Sao_Paulo")
		if err != nil {
			log.Fatalf("load timezone: %v", err)
		}
		planner = briefing.NewBuilder(briefing.NewMapsClient(key), loc)
		blocks = briefing.SimulatedBlocks{}
		mode := "simulated"
		if calID := os.Getenv("CALENDAR_ID"); calID != "" {
			if g, err := briefing.NewGoogleCalendarBlocks(ctx, calID); err != nil {
				slog.Warn("google calendar unavailable; departure blocks simulated", "err", err)
			} else {
				blocks = g
				mode = "google:" + calID
			}
		}
		slog.Info("daily briefing enabled", "tz", loc.String(), "flood_points", len(briefing.HistoricFloodPoints()), "calendar", mode)
	} else {
		slog.Warn("MAPS_API_KEY not set; daily briefing disabled")
	}

	// Voice: the browser streams audio straight to the Gemini Live API and
	// posts the model's function calls back to us, where they run the same
	// tools the ADK graph runs.
	deps := agents.Deps{
		Store:    st,
		UserID:   func(agent.Context) string { return store.DemoUserID },
		Briefing: planner,
		Blocks:   blocks,
	}
	live := httpapi.LiveDeps{
		Tools:             deps.LiveTools(),
		Model:             cmp.Or(os.Getenv("LIVE_MODEL"), "gemini-3.1-flash-live-preview"),
		Voice:             cmp.Or(os.Getenv("LIVE_VOICE"), "Zephyr"),
		SystemInstruction: agents.LiveSystemInstruction,
	}
	if key := os.Getenv("GOOGLE_API_KEY"); key != "" {
		gc, err := genai.NewClient(ctx, &genai.ClientConfig{APIKey: key, Backend: genai.BackendGeminiAPI})
		if err != nil {
			slog.Warn("voice disabled: genai client", "err", err)
		} else {
			live.Client = gc
			slog.Info("live voice enabled", "model", live.Model, "voice", live.Voice, "tools", len(live.Tools))
		}
	} else {
		slog.Warn("GOOGLE_API_KEY not set; live voice disabled")
	}

	api := &httpapi.Handler{
		Store:    st,
		Trusted:  surface,
		Briefing: planner,
		Blocks:   blocks,
		Live:     live,
		UserID:   func(*http.Request) string { return store.DemoUserID },
	}
	api.Register(mux)

	if err := mountChat(ctx, mux, deps); err != nil {
		// Dashboard + consent must come up even without a model key.
		slog.Warn("chat API disabled (no model?)", "err", err)
	}

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

func mountChat(ctx context.Context, mux *http.ServeMux, d agents.Deps) error {
	llm, err := gemini.NewModel(ctx, cmp.Or(os.Getenv("GEMINI_MODEL"), "gemini-3.5-flash"), &genai.ClientConfig{})
	if err != nil {
		return err
	}
	root, err := agents.New(llm, d)
	if err != nil {
		return err
	}
	srv, err := adkrest.NewServer(adkrest.ServerConfig{
		AgentLoader:     agent.NewSingleLoader(root),
		SessionService:  session.InMemoryService(),
		SSEWriteTimeout: 120 * time.Second,
	})
	if err != nil {
		return err
	}
	mux.Handle("/api/", http.StripPrefix("/api", srv))
	return nil
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
