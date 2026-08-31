// Package fsession stores ADK sessions in Cloud Firestore, so a conversation
// with the agent survives the process that hosted it: a Cloud Run revision
// rolling over, a cold start, a second instance.
//
// It implements google.golang.org/adk/v2/session.Service against the contract
// the built-in services honour (docs/research/adk-go-v2-cheatsheet.md §5.2):
// partial events are never persisted, state keys are scoped by prefix
// (app:/user:/temp:), temp: is dropped, and Get/List return the merged map
// with the prefixes re-attached.
//
// Layout, with the default prefix:
//
//	adk_sessions/{app|user|id}                 app_name, user_id, state_json, updated_at, event_count
//	adk_sessions/{app|user|id}/events/{000007-uuid}   payload (JSON), ts
//	adk_app_state/{app}                        state_json
//	adk_user_state/{app|user}                  state_json
//
// State travels as JSON text rather than a Firestore map: session state is
// arbitrary JSON, and Firestore rejects nested arrays.
package fsession

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"google.golang.org/adk/v2/platform"
	"google.golang.org/adk/v2/session"
)

// Service is a Firestore-backed session.Service.
type Service struct {
	client *firestore.Client
	// prefix namespaces the three collections ("adk" by default), so the
	// agent's sessions never collide with the app's own documents.
	prefix string
}

var _ session.Service = (*Service)(nil)

// New opens a Firestore client with Application Default Credentials. database
// is "" for the project's (default) database.
func New(ctx context.Context, projectID, database, prefix string) (*Service, error) {
	if projectID == "" {
		return nil, errors.New("fsession: project id is required")
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
		return nil, fmt.Errorf("fsession: firestore client: %w", err)
	}
	if prefix == "" {
		prefix = "adk"
	}
	return &Service{client: c, prefix: prefix}, nil
}

// Close releases the Firestore client.
func (s *Service) Close() error { return s.client.Close() }

func (s *Service) sessions() *firestore.CollectionRef {
	return s.client.Collection(s.prefix + "_sessions")
}
func (s *Service) appStates() *firestore.CollectionRef {
	return s.client.Collection(s.prefix + "_app_state")
}
func (s *Service) userStates() *firestore.CollectionRef {
	return s.client.Collection(s.prefix + "_user_state")
}

// key joins the identity tuple into one document id. Every part is escaped,
// so a user id containing a slash cannot reach into another collection.
func key(parts ...string) string {
	esc := make([]string, len(parts))
	for i, p := range parts {
		esc[i] = url.QueryEscape(p)
	}
	return strings.Join(esc, "|")
}

func (s *Service) sessionDoc(app, user, id string) *firestore.DocumentRef {
	return s.sessions().Doc(key(app, user, id))
}

// Create stores a new session. It fails when one already exists under the
// same identity, matching the built-in services.
func (s *Service) Create(ctx context.Context, req *session.CreateRequest) (*session.CreateResponse, error) {
	if req.AppName == "" || req.UserID == "" {
		return nil, fmt.Errorf("app_name and user_id are required, got app_name: %q, user_id: %q", req.AppName, req.UserID)
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = platform.NewUUID(ctx)
	}
	now := platform.Now(ctx)
	appDelta, userDelta, sessState := extractStateDeltas(req.State)
	if sessState == nil {
		sessState = map[string]any{}
	}

	doc := s.sessionDoc(req.AppName, req.UserID, sessionID)
	appDoc, userDoc := s.appStates().Doc(key(req.AppName)), s.userStates().Doc(key(req.AppName, req.UserID))
	var appState, userState map[string]any
	err := s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Firestore transactions take every read before the first write.
		if _, err := tx.Get(doc); err == nil {
			return fmt.Errorf("session %s already exists", sessionID)
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		var err error
		if appState, err = readStateTx(tx, appDoc); err != nil {
			return err
		}
		if userState, err = readStateTx(tx, userDoc); err != nil {
			return err
		}
		state, err := encodeState(sessState)
		if err != nil {
			return err
		}
		if err := writeStateTx(tx, appDoc, appState, appDelta); err != nil {
			return err
		}
		if err := writeStateTx(tx, userDoc, userState, userDelta); err != nil {
			return err
		}
		return tx.Set(doc, map[string]any{
			"app_name": req.AppName, "user_id": req.UserID, "session_id": sessionID,
			"state_json": state, "updated_at": now, "event_count": int64(0),
		})
	})
	if err != nil {
		return nil, err
	}
	return &session.CreateResponse{Session: &fsSession{
		appName: req.AppName, userID: req.UserID, sessionID: sessionID,
		state: mergeStates(appState, userState, sessState), updatedAt: now,
	}}, nil
}

// Get reads one session with its events, newest state merged in.
func (s *Service) Get(ctx context.Context, req *session.GetRequest) (*session.GetResponse, error) {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return nil, fmt.Errorf("app_name, user_id, session_id are required, got app_name: %q, user_id: %q, session_id: %q",
			req.AppName, req.UserID, req.SessionID)
	}
	doc := s.sessionDoc(req.AppName, req.UserID, req.SessionID)
	snap, err := doc.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return nil, fmt.Errorf("session %q not found", req.SessionID)
		}
		return nil, err
	}
	sess, err := s.sessionFrom(ctx, snap)
	if err != nil {
		return nil, err
	}
	events, err := s.readEvents(ctx, doc)
	if err != nil {
		return nil, err
	}
	// Filters, in the order the built-ins apply them.
	if !req.After.IsZero() {
		kept := events[:0:0]
		for _, e := range events {
			if !e.Timestamp.Before(req.After) {
				kept = append(kept, e)
			}
		}
		events = kept
	}
	if req.NumRecentEvents > 0 && len(events) > req.NumRecentEvents {
		events = events[len(events)-req.NumRecentEvents:]
	}
	sess.events = events
	return &session.GetResponse{Session: sess}, nil
}

// List returns the sessions of an app (optionally one user's), without events.
func (s *Service) List(ctx context.Context, req *session.ListRequest) (*session.ListResponse, error) {
	if req.AppName == "" {
		return nil, fmt.Errorf("app_name is required, got app_name: %q", req.AppName)
	}
	q := s.sessions().Where("app_name", "==", req.AppName)
	if req.UserID != "" {
		q = q.Where("user_id", "==", req.UserID)
	}
	out := &session.ListResponse{Sessions: make([]session.Session, 0)}
	it := q.Documents(ctx)
	defer it.Stop()
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return nil, err
		}
		sess, err := s.sessionFrom(ctx, snap)
		if err != nil {
			return nil, err
		}
		out.Sessions = append(out.Sessions, sess)
	}
	return out, nil
}

// Delete removes a session and every event under it.
func (s *Service) Delete(ctx context.Context, req *session.DeleteRequest) error {
	if req.AppName == "" || req.UserID == "" || req.SessionID == "" {
		return fmt.Errorf("app_name, user_id, session_id are required, got app_name: %q, user_id: %q, session_id: %q",
			req.AppName, req.UserID, req.SessionID)
	}
	doc := s.sessionDoc(req.AppName, req.UserID, req.SessionID)
	it := doc.Collection("events").Documents(ctx)
	defer it.Stop()
	bw := s.client.BulkWriter(ctx)
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
	_, err := doc.Delete(ctx)
	return err
}

// AppendEvent persists one event and the state it carries. Partial events —
// the streaming chunks — are never stored.
func (s *Service) AppendEvent(ctx context.Context, cur session.Session, ev *session.Event) error {
	if ev == nil || ev.Partial {
		return nil
	}
	sess, ok := cur.(*fsSession)
	if !ok {
		return fmt.Errorf("fsession: unexpected session type %T", cur)
	}
	appDelta, userDelta, sessDelta := extractStateDeltas(ev.Actions.StateDelta)
	stored := trimTemp(ev)
	payload, err := json.Marshal(stored)
	if err != nil {
		return fmt.Errorf("fsession: encoding event: %w", err)
	}
	doc := s.sessionDoc(sess.appName, sess.userID, sess.sessionID)
	appDoc, userDoc := s.appStates().Doc(key(sess.appName)), s.userStates().Doc(key(sess.appName, sess.userID))

	err = s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		// Reads first, writes after: Firestore rejects a read that follows a
		// write in the same transaction.
		snap, err := tx.Get(doc)
		if err != nil {
			return err
		}
		state, err := decodeState(snap)
		if err != nil {
			return err
		}
		appState, err := readStateTx(tx, appDoc)
		if err != nil {
			return err
		}
		userState, err := readStateTx(tx, userDoc)
		if err != nil {
			return err
		}
		maps.Copy(state, sessDelta)
		encoded, err := encodeState(state)
		if err != nil {
			return err
		}
		count, _ := snap.Data()["event_count"].(int64)
		if err := writeStateTx(tx, appDoc, appState, appDelta); err != nil {
			return err
		}
		if err := writeStateTx(tx, userDoc, userState, userDelta); err != nil {
			return err
		}
		// Document ids sort in insertion order, so reading events back needs
		// no index and no second sort key.
		evDoc := doc.Collection("events").Doc(fmt.Sprintf("%012d-%s", count, nonEmpty(ev.ID, platform.NewUUID(ctx))))
		if err := tx.Set(evDoc, map[string]any{"payload": string(payload), "ts": ev.Timestamp}); err != nil {
			return err
		}
		return tx.Set(doc, map[string]any{
			"state_json": encoded, "updated_at": ev.Timestamp, "event_count": count + 1,
		}, firestore.MergeAll)
	})
	if err != nil {
		return err
	}

	sess.mu.Lock()
	maps.Copy(sess.state, sessDelta)
	for k, v := range appDelta {
		sess.state[session.KeyPrefixApp+k] = v
	}
	for k, v := range userDelta {
		sess.state[session.KeyPrefixUser+k] = v
	}
	sess.events = append(sess.events, stored)
	sess.updatedAt = ev.Timestamp
	sess.mu.Unlock()
	return nil
}

// sessionFrom rebuilds a session (state merged, no events) from its document.
func (s *Service) sessionFrom(ctx context.Context, snap *firestore.DocumentSnapshot) (*fsSession, error) {
	data := snap.Data()
	app, _ := data["app_name"].(string)
	user, _ := data["user_id"].(string)
	id, _ := data["session_id"].(string)
	sessState, err := decodeState(snap)
	if err != nil {
		return nil, err
	}
	appState, err := s.readState(ctx, s.appStates().Doc(key(app)))
	if err != nil {
		return nil, err
	}
	userState, err := s.readState(ctx, s.userStates().Doc(key(app, user)))
	if err != nil {
		return nil, err
	}
	updated, _ := data["updated_at"].(time.Time)
	return &fsSession{
		appName: app, userID: user, sessionID: id,
		state: mergeStates(appState, userState, sessState), updatedAt: updated,
	}, nil
}

func (s *Service) readEvents(ctx context.Context, doc *firestore.DocumentRef) ([]*session.Event, error) {
	it := doc.Collection("events").OrderBy(firestore.DocumentID, firestore.Asc).Documents(ctx)
	defer it.Stop()
	var out []*session.Event
	for {
		snap, err := it.Next()
		if errors.Is(err, iterator.Done) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		raw, _ := snap.Data()["payload"].(string)
		ev := &session.Event{}
		if err := json.Unmarshal([]byte(raw), ev); err != nil {
			return nil, fmt.Errorf("fsession: decoding event %s: %w", snap.Ref.ID, err)
		}
		out = append(out, ev)
	}
}

func (s *Service) readState(ctx context.Context, doc *firestore.DocumentRef) (map[string]any, error) {
	snap, err := doc.Get(ctx)
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return map[string]any{}, nil
		}
		return nil, err
	}
	return decodeState(snap)
}

// readStateTx reads an app- or user-scoped state document inside a
// transaction; a missing document is empty state, not an error.
func readStateTx(tx *firestore.Transaction, doc *firestore.DocumentRef) (map[string]any, error) {
	snap, err := tx.Get(doc)
	switch {
	case err == nil:
		return decodeState(snap)
	case status.Code(err) == codes.NotFound:
		return map[string]any{}, nil
	default:
		return nil, err
	}
}

// writeStateTx applies a delta onto state already read, and writes it back.
// It also updates the map in place, so the caller returns the merged view.
func writeStateTx(tx *firestore.Transaction, doc *firestore.DocumentRef, state, delta map[string]any) error {
	if len(delta) == 0 {
		return nil
	}
	maps.Copy(state, delta)
	encoded, err := encodeState(state)
	if err != nil {
		return err
	}
	return tx.Set(doc, map[string]any{"state_json": encoded}, firestore.MergeAll)
}

func encodeState(state map[string]any) (string, error) {
	if len(state) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("fsession: encoding state: %w", err)
	}
	return string(b), nil
}

func decodeState(snap *firestore.DocumentSnapshot) (map[string]any, error) {
	raw, _ := snap.Data()["state_json"].(string)
	state := map[string]any{}
	if raw == "" {
		return state, nil
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, fmt.Errorf("fsession: decoding state of %s: %w", snap.Ref.ID, err)
	}
	return state, nil
}

// extractStateDeltas splits a delta by key prefix. temp: is dropped: it
// belongs to the invocation, never to storage.
func extractStateDeltas(delta map[string]any) (app, user, sess map[string]any) {
	app, user, sess = map[string]any{}, map[string]any{}, map[string]any{}
	for k, v := range delta {
		switch {
		case strings.HasPrefix(k, session.KeyPrefixApp):
			app[strings.TrimPrefix(k, session.KeyPrefixApp)] = v
		case strings.HasPrefix(k, session.KeyPrefixUser):
			user[strings.TrimPrefix(k, session.KeyPrefixUser)] = v
		case !strings.HasPrefix(k, session.KeyPrefixTemp):
			sess[k] = v
		}
	}
	return app, user, sess
}

// mergeStates is the inverse: one map with the prefixes re-attached.
func mergeStates(app, user, sess map[string]any) map[string]any {
	out := make(map[string]any, len(app)+len(user)+len(sess))
	maps.Copy(out, sess)
	for k, v := range app {
		out[session.KeyPrefixApp+k] = v
	}
	for k, v := range user {
		out[session.KeyPrefixUser+k] = v
	}
	return out
}

func trimTemp(ev *session.Event) *session.Event {
	if len(ev.Actions.StateDelta) == 0 {
		return ev
	}
	kept := make(map[string]any, len(ev.Actions.StateDelta))
	for k, v := range ev.Actions.StateDelta {
		if !strings.HasPrefix(k, session.KeyPrefixTemp) {
			kept[k] = v
		}
	}
	if len(kept) == len(ev.Actions.StateDelta) {
		return ev
	}
	cp := *ev
	cp.Actions.StateDelta = kept
	return &cp
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// fsSession is the Session handed to callers. It carries the merged state and
// the events read so far; AppendEvent keeps it in step with Firestore.
type fsSession struct {
	appName   string
	userID    string
	sessionID string

	mu        sync.RWMutex
	state     map[string]any
	events    []*session.Event
	updatedAt time.Time
}

var _ session.Session = (*fsSession)(nil)

func (s *fsSession) ID() string      { return s.sessionID }
func (s *fsSession) AppName() string { return s.appName }
func (s *fsSession) UserID() string  { return s.userID }

func (s *fsSession) State() session.State { return &fsState{mu: &s.mu, state: s.state} }

func (s *fsSession) Events() session.Events {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fsEvents(slices.Clone(s.events))
}

func (s *fsSession) LastUpdateTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}

type fsEvents []*session.Event

func (e fsEvents) All() iter.Seq[*session.Event] {
	return func(yield func(*session.Event) bool) {
		for _, ev := range e {
			if !yield(ev) {
				return
			}
		}
	}
}
func (e fsEvents) Len() int { return len(e) }
func (e fsEvents) At(i int) *session.Event {
	if i >= 0 && i < len(e) {
		return e[i]
	}
	return nil
}

type fsState struct {
	mu    *sync.RWMutex
	state map[string]any
}

var _ session.State = (*fsState)(nil)

func (s *fsState) Get(k string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.state[k]
	if !ok {
		return nil, session.ErrStateKeyNotExist
	}
	return v, nil
}

func (s *fsState) Set(k string, v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state[k] = v
	return nil
}

func (s *fsState) All() iter.Seq2[string, any] {
	s.mu.RLock()
	clone := maps.Clone(s.state)
	s.mu.RUnlock()
	return func(yield func(string, any) bool) {
		for k, v := range clone {
			if !yield(k, v) {
				return
			}
		}
	}
}
