package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestMemoryCRUD(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()

	if _, err := m.GetUser(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}

	u := User{ID: "u1", Name: "Ana", HourlyRateCents: 50_00}
	if err := m.PutUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := m.GetUser(ctx, "u1")
	if err != nil || got.HourlyRateCents != 50_00 {
		t.Fatalf("get user: %v %+v", err, got)
	}

	if err := m.PutTask(ctx, "u1", Task{ID: "t1", Name: "dishes", EstMinutes: 60, FreqPerMon: 30}); err != nil {
		t.Fatal(err)
	}
	tasks, _ := m.ListTasks(ctx, "u1")
	if len(tasks) != 1 || tasks[0].Name != "dishes" {
		t.Fatalf("tasks = %+v", tasks)
	}
	if err := m.DeleteTask(ctx, "u1", "t1"); err != nil {
		t.Fatal(err)
	}
	if err := m.DeleteTask(ctx, "u1", "t1"); !errors.Is(err, ErrNotFound) {
		t.Fatal("double delete should be ErrNotFound")
	}
}

func TestSeedDemo(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	if err := SeedDemo(ctx, m, now); err != nil {
		t.Fatal(err)
	}

	u, err := m.GetUser(ctx, DemoUserID)
	if err != nil {
		t.Fatal(err)
	}
	if u.HourlyRateCents <= 0 {
		t.Fatal("demo user has no rate")
	}
	tasks, _ := m.ListTasks(ctx, DemoUserID)
	if len(tasks) < 4 {
		t.Fatalf("expected seeded tasks, got %d", len(tasks))
	}
	ledger, _ := m.ListLedger(ctx, DemoUserID)
	if len(ledger) != 4 {
		t.Fatalf("expected 4 ledger weeks, got %d", len(ledger))
	}
	// ledger sorted ascending by week, oldest confirmed, newest projected
	if !ledger[0].Confirmed || ledger[len(ledger)-1].Confirmed {
		t.Fatalf("confirmed flags wrong: first=%v last=%v", ledger[0].Confirmed, ledger[len(ledger)-1].Confirmed)
	}
	for _, e := range ledger {
		if e.WeekStart.Weekday() != time.Monday {
			t.Fatalf("week start not Monday: %v", e.WeekStart)
		}
	}
	plans, _ := m.ListActionPlans(ctx, DemoUserID)
	if len(plans) != 1 || plans[0].Status != PlanDrifting {
		t.Fatalf("expected one drifting plan, got %+v", plans)
	}
}

func TestMemoryConcurrentAccess(t *testing.T) {
	// -race is mandatory in CI; hammer the store from many goroutines.
	ctx := context.Background()
	m := NewMemory()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = m.PutTask(ctx, "u", Task{ID: string(rune('a' + i%26)), Name: "x", EstMinutes: 1, FreqPerMon: 1})
		}(i)
		go func() {
			defer wg.Done()
			_, _ = m.ListTasks(ctx, "u")
		}()
	}
	wg.Wait()
}
