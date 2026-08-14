package catalog

import "testing"

func TestMatchDishes(t *testing.T) {
	got := Match("Lavar louça depois do jantar", Seed())
	if len(got) == 0 {
		t.Fatal("no match for louça")
	}
	if got[0].ID != "dishwasher" {
		t.Fatalf("expected dishwasher first (executable), got %s", got[0].ID)
	}
}

func TestMatchExecutablesFirst(t *testing.T) {
	// "boleto" matches executable boleto-pile and advised auto-pay
	got := Match("pagar boleto da luz", Seed())
	if len(got) < 2 {
		t.Fatalf("expected >=2 matches, got %d", len(got))
	}
	if got[0].Class != ClassExecutable {
		t.Fatalf("executables must rank first, got %s (%s)", got[0].ID, got[0].Class)
	}
}

func TestMatchEnglish(t *testing.T) {
	if got := Match("washing dishes every day", Seed()); len(got) == 0 || got[0].ID != "dishwasher" {
		t.Fatal("english trigger failed")
	}
}

func TestNoMatch(t *testing.T) {
	if got := Match("skydiving lessons", Seed()); len(got) != 0 {
		t.Fatalf("unexpected match: %+v", got)
	}
}

func TestSeedIntegrity(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Seed() {
		if r.ID == "" || r.Title == "" || len(r.Triggers) == 0 {
			t.Errorf("recipe %+v missing id/title/triggers", r)
		}
		if seen[r.ID] {
			t.Errorf("duplicate recipe id %s", r.ID)
		}
		seen[r.ID] = true
		if r.Class == ClassExecutable && r.Capability == "" {
			t.Errorf("executable recipe %s missing capability", r.ID)
		}
		if r.Capability == CapAP2Purchase && r.Class != ClassRoadmap && r.ProductID == "" {
			t.Errorf("ap2 recipe %s missing product id", r.ID)
		}
	}
}
