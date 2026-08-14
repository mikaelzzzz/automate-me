package store

import (
	"context"
	"sort"
	"sync"
)

// Memory is the in-memory Store used by DEMO_MODE=seed and tests.
type Memory struct {
	mu        sync.RWMutex
	users     map[string]User
	tasks     map[string]map[string]Task // userID -> taskID -> Task
	proposals map[string]Proposal
	mandates  map[string]MandateRecord
	ledger    map[string]LedgerEntry
	plans     map[string]ActionPlan
	briefings map[string]BriefingCard
}

var _ Store = (*Memory)(nil)

func NewMemory() *Memory {
	return &Memory{
		users:     map[string]User{},
		tasks:     map[string]map[string]Task{},
		proposals: map[string]Proposal{},
		mandates:  map[string]MandateRecord{},
		ledger:    map[string]LedgerEntry{},
		plans:     map[string]ActionPlan{},
		briefings: map[string]BriefingCard{},
	}
}

func (m *Memory) GetUser(_ context.Context, id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

func (m *Memory) PutUser(_ context.Context, u User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.users[u.ID] = u
	return nil
}

func (m *Memory) ListTasks(_ context.Context, userID string) ([]Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Task
	for _, t := range m.tasks[userID] {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) PutTask(_ context.Context, userID string, t Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tasks[userID] == nil {
		m.tasks[userID] = map[string]Task{}
	}
	m.tasks[userID][t.ID] = t
	return nil
}

func (m *Memory) DeleteTask(_ context.Context, userID, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[userID][taskID]; !ok {
		return ErrNotFound
	}
	delete(m.tasks[userID], taskID)
	return nil
}

func (m *Memory) ListProposals(_ context.Context, userID string) ([]Proposal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Proposal
	for _, p := range m.proposals {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) PutProposal(_ context.Context, p Proposal) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.proposals[p.ID] = p
	return nil
}

func (m *Memory) GetProposal(_ context.Context, id string) (Proposal, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.proposals[id]
	if !ok {
		return Proposal{}, ErrNotFound
	}
	return p, nil
}

func (m *Memory) PutMandateRecord(_ context.Context, rec MandateRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.mandates[rec.ID] = rec
	return nil
}

func (m *Memory) ListMandateRecords(_ context.Context, userID string) ([]MandateRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []MandateRecord
	for _, r := range m.mandates {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) ListLedger(_ context.Context, userID string) ([]LedgerEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []LedgerEntry
	for _, e := range m.ledger {
		if e.UserID == userID {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WeekStart.Before(out[j].WeekStart) })
	return out, nil
}

func (m *Memory) PutLedgerEntry(_ context.Context, e LedgerEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ledger[e.ID] = e
	return nil
}

func (m *Memory) ListActionPlans(_ context.Context, userID string) ([]ActionPlan, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ActionPlan
	for _, p := range m.plans {
		if p.UserID == userID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *Memory) PutActionPlan(_ context.Context, p ActionPlan) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.plans[p.ID] = p
	return nil
}

func (m *Memory) ListBriefing(_ context.Context, userID, day string) ([]BriefingCard, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []BriefingCard
	for _, c := range m.briefings {
		if c.UserID == userID && c.Day == day {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].EventStart.Before(out[j].EventStart) })
	return out, nil
}

func (m *Memory) PutBriefingCard(_ context.Context, c BriefingCard) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.briefings[c.ID] = c
	return nil
}
