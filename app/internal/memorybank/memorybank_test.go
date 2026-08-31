package memorybank

import (
	"context"
	"os"
	"testing"
)

// TestRecallAgainstMemoryBank talks to a real Agent Engine. It is a smoke
// test, not a fixture: memory content is whatever the bank has learned.
//
//	MEMORY_TEST_PROJECT=automate-me-hack MEMORY_TEST_ENGINE=<id> go test ./internal/memorybank/
func TestRecallAgainstMemoryBank(t *testing.T) {
	project, engine := os.Getenv("MEMORY_TEST_PROJECT"), os.Getenv("MEMORY_TEST_ENGINE")
	if project == "" || engine == "" {
		t.Skip("set MEMORY_TEST_PROJECT and MEMORY_TEST_ENGINE to run against Memory Bank")
	}
	ctx := context.Background()
	s, err := New(ctx, project, os.Getenv("MEMORY_TEST_LOCATION"), engine)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	defer s.Close()

	user := os.Getenv("MEMORY_TEST_USER")
	if user == "" {
		user = "demo"
	}
	facts, err := s.Recall(ctx, user, recallSmokeQuery)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	t.Logf("recalled %d fact(s) for %q", len(facts), user)
	for _, f := range facts {
		t.Logf("  · %s", f)
	}
}

const recallSmokeQuery = "the user's routine, constraints, household, work and how they prefer the agent to talk to them"
