package engine

import (
	"math"
	"testing"
)

func TestCostOfInactionCents(t *testing.T) {
	tests := []struct {
		name string
		task Task
		rate int64
		want int64
	}{
		{"dishes 60min daily at R$50/h", Task{"dishes", 60, 30}, 50_00, 1500_00},
		{"dishes 45min daily at R$50/h", Task{"dishes", 45, 30}, 50_00, 1125_00},
		{"weekly errand 90min at R$80/h", Task{"errands", 90, 4.33}, 80_00, 519_60},
		{"zero minutes", Task{"noop", 0, 30}, 50_00, 0},
		{"zero freq", Task{"noop", 60, 0}, 50_00, 0},
		{"zero rate", Task{"dishes", 60, 30}, 0, 0},
		{"negative minutes", Task{"bad", -10, 30}, 50_00, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CostOfInactionCents(tt.task, tt.rate); got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEvaluate(t *testing.T) {
	rate := int64(50_00) // R$50/h

	t.Run("dishwasher: R$3000 upfront, saves 55min/day", func(t *testing.T) {
		a := Automation{Name: "dishwasher", UpfrontCents: 3000_00, MinutesSavedPerOcc: 55, FreqPerMonth: 30}
		ev := Evaluate(a, rate)
		if !ev.Proposable {
			t.Fatal("should be proposable")
		}
		if ev.MonthlySavingsCents != 1375_00 {
			t.Errorf("savings = %d, want 137500", ev.MonthlySavingsCents)
		}
		if got, want := ev.PaybackMonths, 3000_00/1375.00/100; math.Abs(got-want) > 1e-9 {
			t.Errorf("payback = %v, want %v", got, want)
		}
	})

	t.Run("zero-upfront subscription ranks by net savings with payback 0", func(t *testing.T) {
		a := Automation{Name: "grocery-delivery", MonthlyRunningCents: 80_00, MinutesSavedPerOcc: 120, FreqPerMonth: 4.33}
		ev := Evaluate(a, rate)
		if !ev.Proposable || ev.PaybackMonths != 0 {
			t.Fatalf("want proposable payback 0, got %+v", ev)
		}
		if ev.NetMonthlyCents != 433_00-80_00 {
			t.Errorf("net = %d, want %d", ev.NetMonthlyCents, 433_00-80_00)
		}
	})

	t.Run("negative net is never proposable", func(t *testing.T) {
		a := Automation{Name: "bad-deal", UpfrontCents: 100_00, MonthlyRunningCents: 500_00, MinutesSavedPerOcc: 10, FreqPerMonth: 4}
		ev := Evaluate(a, rate)
		if ev.Proposable {
			t.Fatal("negative-net automation must not be proposable")
		}
		if !math.IsInf(ev.PaybackMonths, 1) {
			t.Errorf("payback should be +Inf, got %v", ev.PaybackMonths)
		}
	})

	t.Run("break-even net is not proposable", func(t *testing.T) {
		// saves exactly its running cost: 60min/mo at R$50/h = R$50 = running
		a := Automation{Name: "break-even", MonthlyRunningCents: 50_00, MinutesSavedPerOcc: 60, FreqPerMonth: 1}
		if ev := Evaluate(a, rate); ev.Proposable {
			t.Fatal("net == 0 must not be proposable")
		}
	})
}

func TestRank(t *testing.T) {
	rate := int64(50_00)
	mk := func(name string, upfront, running int64, minutes int, freq float64) Candidate {
		a := Automation{Name: name, UpfrontCents: upfront, MonthlyRunningCents: running, MinutesSavedPerOcc: minutes, FreqPerMonth: freq}
		return Candidate{Automation: a, Eval: Evaluate(a, rate)}
	}
	cands := []Candidate{
		mk("dishwasher", 3000_00, 0, 55, 30),     // payback ≈ 2.18
		mk("robot-vacuum", 2000_00, 0, 30, 8),    // payback = 10
		mk("delivery-sub", 0, 80_00, 120, 4.33),  // payback 0, net 353
		mk("cleaner", 0, 400_00, 240, 4.33),      // payback 0, net 466
		mk("bad-deal", 100_00, 500_00, 10, 4),    // not proposable
	}
	got := Rank(cands)
	wantOrder := []string{"cleaner", "delivery-sub", "dishwasher", "robot-vacuum"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len = %d, want %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].Automation.Name != w {
			t.Errorf("pos %d = %s, want %s", i, got[i].Automation.Name, w)
		}
	}
}

func TestBuybackRateCents(t *testing.T) {
	// R$120,000/year ÷ 2000 ÷ 4 = R$15/h
	if got := BuybackRateCents(120_000_00); got != 15_00 {
		t.Errorf("got %d, want 1500", got)
	}
	if got := BuybackRateCents(0); got != 0 {
		t.Errorf("zero income should give 0, got %d", got)
	}
}

func TestTrafficCostCents(t *testing.T) {
	// 28min in traffic vs 14min free-flow at R$60/h → 14min = R$14
	if got := TrafficCostCents(1680, 840, 60_00); got != 14_00 {
		t.Errorf("got %d, want 1400", got)
	}
	if got := TrafficCostCents(800, 840, 60_00); got != 0 {
		t.Errorf("free-flow should cost 0, got %d", got)
	}
}
