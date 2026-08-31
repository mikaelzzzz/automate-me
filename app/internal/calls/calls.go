// Package calls keeps what was said out loud. The Live API holds a
// conversation in the browser tab and nowhere else: close the tab and the call
// is gone. This is where it survives — in Firestore when configured, in memory
// otherwise — so reopening Talk shows the conversation you were having.
package calls

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// maxStoredTurns bounds one read of a user's call history. Long enough to
// hold several conversations, short enough that reopening Talk is one small
// query.
const maxStoredTurns = 400

// Turn is one spoken exchange, as the browser transcribed it.
type Turn struct {
	Role string    `json:"role"` // user | model
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Store persists call transcripts per user.
type Store interface {
	// Append adds the turns of one finished call.
	Append(ctx context.Context, userID string, turns []Turn) error
	// Recent returns the last n turns, oldest first.
	Recent(ctx context.Context, userID string, n int) ([]Turn, error)
	// Clear forgets every call of a user.
	Clear(ctx context.Context, userID string) error
}

// Memory is the fallback store: good enough for one process, gone with it.
type Memory struct {
	mu    sync.RWMutex
	turns map[string][]Turn
}

func NewMemory() *Memory { return &Memory{turns: map[string][]Turn{}} }

func (m *Memory) Append(_ context.Context, userID string, turns []Turn) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns[userID] = append(m.turns[userID], turns...)
	return nil
}

func (m *Memory) Recent(_ context.Context, userID string, n int) ([]Turn, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	all := m.turns[userID]
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return append([]Turn(nil), all...), nil
}

func (m *Memory) Clear(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.turns, userID)
	return nil
}

// Firestore keeps calls under one document per user, one subcollection entry
// per turn. Document ids are the turn's timestamp in nanoseconds, so reading
// them back in id order is reading them back in the order they were spoken —
// no index, no second sort key.
type Firestore struct {
	client *firestore.Client
	col    string
}

func NewFirestore(ctx context.Context, projectID, database, prefix string) (*Firestore, error) {
	if projectID == "" {
		return nil, errors.New("calls: project id is required")
	}
	var (
		c   *firestore.Client
		err error
	)
	if database == "" || database == "(default)" {
		c, err = firestore.NewClient(ctx, projectID)
	} else {
		c, err = firestore.NewClientWithDatabase(ctx, projectID, database)
	}
	if err != nil {
		return nil, fmt.Errorf("calls: firestore client: %w", err)
	}
	if prefix == "" {
		prefix = "adk"
	}
	return &Firestore{client: c, col: prefix + "_calls"}, nil
}

func (f *Firestore) Close() error { return f.client.Close() }

func (f *Firestore) turns(userID string) *firestore.CollectionRef {
	return f.client.Collection(f.col).Doc(userID).Collection("turns")
}

func (f *Firestore) Append(ctx context.Context, userID string, turns []Turn) error {
	if len(turns) == 0 {
		return nil
	}
	bw := f.client.BulkWriter(ctx)
	for i, t := range turns {
		if t.At.IsZero() {
			t.At = time.Now().UTC()
		}
		// The nanosecond plus the index keeps two turns in the same instant apart.
		id := fmt.Sprintf("%019d-%03d", t.At.UTC().UnixNano(), i)
		if _, err := bw.Set(f.turns(userID).Doc(id), map[string]any{
			"role": t.Role, "text": t.Text, "at": t.At.UTC(),
		}); err != nil {
			return fmt.Errorf("calls: queue turn: %w", err)
		}
	}
	bw.End()
	// The parent document exists only so the console shows the user; the turns
	// live in the subcollection and are readable without it.
	_, err := f.client.Collection(f.col).Doc(userID).Set(ctx, map[string]any{
		"user_id": userID, "last_call_at": time.Now().UTC(),
	}, firestore.MergeAll)
	return err
}

func (f *Firestore) Recent(ctx context.Context, userID string, n int) ([]Turn, error) {
	// Ascending by document id, then take the tail here. Firestore wants a
	// composite index for a descending __name__ order, and limitToLast is that
	// order server-side — a transcript is not worth an index.
	docs, err := f.turns(userID).OrderBy(firestore.DocumentID, firestore.Asc).Limit(maxStoredTurns).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("calls: read turns: %w", err)
	}
	out := make([]Turn, 0, len(docs))
	for _, snap := range docs {
		d := snap.Data()
		role, _ := d["role"].(string)
		text, _ := d["text"].(string)
		at, _ := d["at"].(time.Time)
		out = append(out, Turn{Role: role, Text: text, At: at})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

func (f *Firestore) Clear(ctx context.Context, userID string) error {
	it := f.turns(userID).Documents(ctx)
	defer it.Stop()
	bw := f.client.BulkWriter(ctx)
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return err
		}
		if _, err := bw.Delete(snap.Ref); err != nil {
			return err
		}
	}
	bw.End()
	_, err := f.client.Collection(f.col).Doc(userID).Delete(ctx)
	return err
}
