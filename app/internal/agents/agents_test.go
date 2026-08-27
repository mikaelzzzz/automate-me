package agents

import (
	"testing"

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
