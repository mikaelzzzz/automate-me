package fsession

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/api/iterator"

	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/sessiontestsuite"
)

// TestFirestoreSessionService runs ADK's own conformance suite against this
// implementation. It needs a Firestore to talk to — the emulator
// (FIRESTORE_EMULATOR_HOST) or a real project:
//
//	FIRESTORE_TEST_PROJECT=automate-me-hack go test ./internal/fsession/
//
// Each setup gets its own collection prefix and drops it afterwards, so runs
// never see each other's documents.
func TestFirestoreSessionService(t *testing.T) {
	project := os.Getenv("FIRESTORE_TEST_PROJECT")
	if project == "" {
		t.Skip("set FIRESTORE_TEST_PROJECT to run the Firestore session conformance suite")
	}
	var n atomic.Int64
	run := time.Now().UTC().Format("20060102T150405")

	sessiontestsuite.RunServiceTests(t, sessiontestsuite.SuiteOptions{
		SupportsUserProvidedSessionID: true,
		ProvidesServerAssignedEventID: false,
		AppName:                       "testApp",
	}, func(t *testing.T) session.Service {
		prefix := fmt.Sprintf("test_%s_%d", run, n.Add(1))
		svc, err := New(context.Background(), project, os.Getenv("FIRESTORE_TEST_DATABASE"), prefix)
		if err != nil {
			t.Fatalf("new firestore session service: %v", err)
		}
		t.Cleanup(func() {
			if err := svc.dropAll(context.Background()); err != nil {
				t.Logf("cleanup %s: %v", prefix, err)
			}
			svc.Close()
		})
		return svc
	})
}

// dropAll removes every document this service wrote. Test-only: it exists so
// a conformance run leaves the project as it found it.
func (s *Service) dropAll(ctx context.Context) error {
	sessions := s.sessions().Documents(ctx)
	defer sessions.Stop()
	for {
		snap, err := sessions.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return err
		}
		events := snap.Ref.Collection("events").Documents(ctx)
		for {
			ev, err := events.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				events.Stop()
				return err
			}
			if _, err := ev.Ref.Delete(ctx); err != nil {
				events.Stop()
				return err
			}
		}
		events.Stop()
		if _, err := snap.Ref.Delete(ctx); err != nil {
			return err
		}
	}
	for _, col := range []string{s.prefix + "_app_state", s.prefix + "_user_state"} {
		it := s.client.Collection(col).Documents(ctx)
		for {
			snap, err := it.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				it.Stop()
				return err
			}
			if _, err := snap.Ref.Delete(ctx); err != nil {
				it.Stop()
				return err
			}
		}
		it.Stop()
	}
	return nil
}
