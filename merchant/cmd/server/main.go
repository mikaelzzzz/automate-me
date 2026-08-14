// The merchant-agent service: deterministic AP2 rail over HTTP plus an A2A
// surface (agent card + conversational catalog skills) when a Gemini model is
// configured. Everything here is a labeled simulation — no real payments.
package main

import (
	"cmp"
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/adk/v2/runner"
	adka2a "google.golang.org/adk/v2/server/adka2a/v2"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"

	"automate-me/ap2core"
	"automate-me/merchant/internal/domain"
	"automate-me/merchant/internal/transport"
)

func main() {
	ctx := context.Background()

	signer, err := ap2core.GenerateSigner("merchant-key-1")
	if err != nil {
		log.Fatalf("generate merchant key: %v", err)
	}
	info := ap2core.Merchant{
		ID:      "automate-me-demo-merchant",
		Name:    "Automate.me Demo Merchant",
		Website: cmp.Or(os.Getenv("PUBLIC_URL"), "http://localhost:8081"),
	}
	m := domain.New(info, signer, domain.DemoCatalog())

	mux := http.NewServeMux()
	(&transport.Handler{M: m}).Register(mux)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	if err := mountA2A(ctx, mux, m); err != nil {
		// A2A is additive; the deterministic AP2 rail must come up regardless.
		slog.Warn("A2A surface disabled", "err", err)
	}

	port := cmp.Or(os.Getenv("PORT"), "8081")
	slog.Info("merchant-agent listening", "port", port, "merchant", info.ID)
	srv := &http.Server{Addr: ":" + port, Handler: mux, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second}
	log.Fatal(srv.ListenAndServe())
}

// mountA2A exposes the conversational side (catalog search, product Q&A) as a
// discoverable A2A agent. Mandates never travel this path.
func mountA2A(ctx context.Context, mux *http.ServeMux, m *domain.Merchant) error {
	llm, err := gemini.NewModel(ctx, cmp.Or(os.Getenv("GEMINI_MODEL"), "gemini-3.5-flash"), &genai.ClientConfig{})
	if err != nil {
		return err
	}

	type searchIn struct {
		Query string `json:"query" jsonschema:"Free-text product search, e.g. 'dishwasher'"`
	}
	type searchOut struct {
		Products []domain.Product `json:"products"`
	}
	searchTool, err := functiontool.New(functiontool.Config{
		Name:        "search_catalog",
		Description: "Search the merchant's product catalog.",
	}, func(_ agent.Context, in searchIn) (searchOut, error) {
		return searchOut{Products: m.SearchCatalog(in.Query)}, nil
	})
	if err != nil {
		return err
	}

	a, err := llmagent.New(llmagent.Config{
		Name:        "merchant",
		Description: "Simulated merchant for Automate.me: searches products and explains offers. Purchases run over the deterministic AP2 endpoints, never through chat.",
		Model:       llm,
		Instruction: "You are the Automate.me demo merchant. Help agents find products with search_catalog and answer questions about them. You cannot take payments in chat: point callers to the AP2 endpoints (/ap2/*). Everything is a simulation.",
		Tools:       []tool.Tool{searchTool},
	})
	if err != nil {
		return err
	}

	card := &a2a.AgentCard{
		Name:        a.Name(),
		Description: a.Description(),
		SupportedInterfaces: []*a2a.AgentInterface{{
			URL:             cmp.Or(os.Getenv("PUBLIC_URL"), "http://localhost:8081") + "/invoke",
			ProtocolBinding: a2a.TransportProtocolJSONRPC,
			ProtocolVersion: a2a.Version,
		}},
		Version:            "1.0.0",
		DefaultInputModes:  []string{"text/plain"},
		DefaultOutputModes: []string{"text/plain"},
		Skills:             adka2a.BuildAgentSkills(a),
		Capabilities:       a2a.AgentCapabilities{Streaming: true},
	}
	exec := adka2a.NewExecutor(adka2a.ExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:           a.Name(),
			Agent:             a,
			SessionService:    session.InMemoryService(),
			AutoCreateSession: true,
		},
	})
	mux.Handle(a2asrv.WellKnownAgentCardPath, a2asrv.NewStaticAgentCardHandler(card))
	mux.Handle("/invoke", a2asrv.NewJSONRPCHandler(a2asrv.NewHandler(exec)))
	return nil
}
