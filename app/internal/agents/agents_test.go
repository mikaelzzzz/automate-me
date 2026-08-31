package agents

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/adk/v2/agent"

	"automate-me/app/internal/store"
)

func TestUpsertTarget(t *testing.T) {
	existing := []store.Task{
		{ID: "t-dishes", Name: "Washing dishes after dinner", Source: "interview"},
		{ID: "t-groceries", Name: "Supermarket run", Source: "photo"},
		{ID: "t-commute", Name: "Commute to the office"},
	}
	cases := []struct {
		name, taskID, newName string
		wantID                string
		wantUpdated           bool
	}{
		{"explicit id wins", "t-dishes", "Washing dishes by hand", "t-dishes", true},
		{"same name, different case/spacing", "", "supermarket  RUN", "t-groceries", true},
		{"unknown id falls back to name match", "t-nope", "Supermarket run", "t-groceries", true},
		{"new task gets name-derived id", "", "Paying boletos", "t-paying-boletos", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, updated := upsertTarget(existing, c.taskID, c.newName)
			if got.ID != c.wantID || updated != c.wantUpdated {
				t.Fatalf("got (%q, %v), want (%q, %v)", got.ID, updated, c.wantID, c.wantUpdated)
			}
		})
	}
}

func TestUpsertTargetAvoidsIDClash(t *testing.T) {
	existing := []store.Task{{ID: "t-dishes", Name: "Dishes tonight"}}
	// Different name whose sanitized form collides with an existing id.
	got, updated := upsertTarget(existing, "", "dishes")
	if updated {
		t.Fatalf("name 'dishes' should not match 'Dishes tonight'")
	}
	if got.ID != "t-dishes-2" {
		t.Fatalf("expected suffixed id, got %q", got.ID)
	}
}

func TestBRL(t *testing.T) {
	cases := map[int64]string{0: "R$0.00", 5: "R$0.05", 150000: "R$1,500.00", 336608: "R$3,366.08", 123456789: "R$1,234,567.89", -32475: "-R$324.75"}
	for cents, want := range cases {
		if got := brl(cents); got != want {
			t.Errorf("brl(%d) = %q, want %q", cents, got, want)
		}
	}
}

func TestApproveResolvesALooseName(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory()
	if err := store.SeedDemo(ctx, st, time.Now()); err != nil {
		t.Fatal(err)
	}
	d := Deps{Store: st, UserID: func(agent.Context) string { return store.DemoUserID }}

	// The voice model guessed an id it never saw. It should still land on the
	// dishwasher, because that is the only proposal matching the word.
	out, err := d.Approve(ctx, store.DemoUserID, approveIn{ProposalID: "prop-dishwasher"})
	if err != nil {
		t.Fatalf("loose match failed: %v", err)
	}
	if out.Status != "approved" || !out.Executable {
		t.Fatalf("got %+v, want an approved executable purchase", out)
	}

	// Nothing resembling this exists: the error has to name the real options
	// so the model can retry rather than apologise.
	_, err = d.Approve(ctx, store.DemoUserID, approveIn{ProposalID: "prop-teleporter"})
	if err == nil {
		t.Fatal("expected an error for an unknown proposal")
	}
	if !strings.Contains(err.Error(), "propose_automations") || !strings.Contains(err.Error(), "dishwasher") {
		t.Fatalf("unhelpful error: %v", err)
	}
}
