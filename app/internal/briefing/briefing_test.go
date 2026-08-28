package briefing

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestDecodePolyline(t *testing.T) {
	// Google's documented example vector.
	pts := DecodePolyline("_p~iF~ps|U_ulLnnqC_mqNvxq`@")
	want := []LatLng{{38.5, -120.2}, {40.7, -120.95}, {43.252, -126.453}}
	if len(pts) != len(want) {
		t.Fatalf("got %d points, want %d", len(pts), len(want))
	}
	for i := range want {
		if math.Abs(pts[i].Lat-want[i].Lat) > 1e-5 || math.Abs(pts[i].Lng-want[i].Lng) > 1e-5 {
			t.Errorf("point %d = %v, want %v", i, pts[i], want[i])
		}
	}
}

func TestDistanceToPath(t *testing.T) {
	// A 1 km east-west segment at São Paulo's latitude; a point 100 m north
	// of its midpoint.
	path := []LatLng{{-23.55, -46.64}, {-23.55, -46.63}}
	p := LatLng{Lat: -23.55 + 100/metresPerDegLat, Lng: -46.635}
	if d := DistanceToPathMetres(p, path); math.Abs(d-100) > 2 {
		t.Fatalf("distance = %.1f m, want ≈100", d)
	}
	// Beyond the segment end: distance grows past 100 m.
	far := LatLng{Lat: -23.55, Lng: -46.62}
	if d := DistanceToPathMetres(far, path); d < 900 {
		t.Fatalf("far distance = %.1f m, want ≈1 km", d)
	}
}

func TestPolygonContains(t *testing.T) {
	var polys []polygon
	raw := json.RawMessage(`{"type":"MultiPolygon","coordinates":[[[[-46.7,-23.6],[-46.5,-23.6],[-46.5,-23.5],[-46.7,-23.5],[-46.7,-23.6]]]]}`)
	polys, err := parseMultiPolygon(raw)
	if err != nil || len(polys) != 1 {
		t.Fatalf("parse: %v (%d polys)", err, len(polys))
	}
	if !polys[0].contains(LatLng{-23.55, -46.6}) {
		t.Error("centre should be inside")
	}
	if polys[0].contains(LatLng{-23.45, -46.6}) {
		t.Error("north of the box should be outside")
	}
	if !pathCrosses([]LatLng{{-23.9, -46.6}, {-23.55, -46.6}}, polys) {
		t.Error("path entering the box should cross")
	}
}

func TestHistoricLayerLoadsAndMatches(t *testing.T) {
	pts := HistoricFloodPoints()
	if len(pts) < 100 {
		t.Fatalf("embedded GeoSampa layer has %d points, expected the full extract", len(pts))
	}
	// A path through the first logged point must find it; a path in the
	// Atlantic must not.
	near := FloodPointsNear([]LatLng{{pts[0].Lat, pts[0].Lng - 0.002}, {pts[0].Lat, pts[0].Lng + 0.002}}, 150)
	if len(near) == 0 {
		t.Fatal("expected a hit on a route passing over a logged flooding")
	}
	if got := FloodPointsNear([]LatLng{{-24.5, -45.0}, {-24.6, -45.1}}, 150); len(got) != 0 {
		t.Fatalf("ocean route matched %d points", len(got))
	}
	for in, want := range map[string]string{"VP - VILA PRUDENTE": "Vila Prudente", "AF - ARICANDUVA/VILA FORMOSA": "Aricanduva/Vila Formosa", "MP - SAO MIGUEL PAULISTA": "Sao Miguel Paulista"} {
		if got := cleanSubprefecture(in); got != want {
			t.Errorf("cleanSubprefecture(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeMaps struct {
	calls int
}

func (f *fakeMaps) ComputeRoute(_ context.Context, _, _ Place, dep time.Time) (Route, error) {
	f.calls++
	// Rush hour costs 20 minutes on a 25-minute static drive.
	d := 25 * time.Minute
	if dep.Hour() >= 7 && dep.Hour() < 10 {
		d = 45 * time.Minute
	}
	return Route{Duration: d, StaticDuration: 25 * time.Minute, DistanceMetres: 12000, Description: "Marginal Pinheiros",
		Path: []LatLng{{-23.60621642, -46.54}, {-23.60621642, -46.5368208}, {-23.60621642, -46.53}}}, nil
}
func (f *fakeMaps) HourlyForecast(_ context.Context, _ LatLng, at time.Time) (Weather, error) {
	return Weather{At: at, TempC: 17, Condition: "Partly sunny", RainChancePct: 55}, nil
}
func (f *fakeMaps) PublicAlerts(_ context.Context, _ LatLng) ([]Alert, error) {
	polys, _ := parseMultiPolygon(json.RawMessage(`{"type":"Polygon","coordinates":[[[-46.9,-23.9],[-46.3,-23.9],[-46.3,-23.3],[-46.9,-23.3],[-46.9,-23.9]]]}`))
	return []Alert{
		{ID: "heat", EventType: "HEAT", Severity: "SEVERE", Title: "Heat wave", polys: polys},
		{ID: "flood", EventType: "FLASH_FLOOD", Severity: "MODERATE", Title: "Flash flooding", Description: "Heavy rain expected. Avoid low areas.", polys: polys},
	}, nil
}

func TestBuildCard(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	fm := &fakeMaps{}
	b := &Builder{Maps: fm, Now: time.Now, Loc: loc, Parallelism: 2, FloodRadiusM: 150, ArriveEarly: 5 * time.Minute}
	start := time.Date(2026, 8, 29, 9, 0, 0, 0, loc)
	cards := b.Build(context.Background(), "demo", 50_00, []Event{{ID: "a", Summary: "Client", Start: start, Origin: "x", Destination: "y"}})
	if len(cards) != 1 {
		t.Fatalf("cards = %d", len(cards))
	}
	c := cards[0]
	if fm.calls != 2 {
		t.Errorf("expected two Routes calls (rough + refined), got %d", fm.calls)
	}
	wantDep := start.Add(-5*time.Minute - 45*time.Minute)
	if !c.DepartureTime.Equal(wantDep) {
		t.Errorf("departure = %v, want %v", c.DepartureTime, wantDep)
	}
	if c.TrafficMinutes != 20 || c.TrafficCents != 16_67 {
		t.Errorf("traffic = %d min / %d cents, want 20 / 1667", c.TrafficMinutes, c.TrafficCents)
	}
	if c.FloodRisk != "alert" || c.FloodPoints == 0 {
		t.Errorf("flood = %q with %d historic points; want alert + historic hits (route passes a logged flooding)", c.FloodRisk, c.FloodPoints)
	}
	if c.Clothing != "a jacket, umbrella · heat alert: carry water" {
		t.Errorf("clothing = %q", c.Clothing)
	}
	if c.AlertHeadline == "" || c.AlternativeNote == "" {
		t.Errorf("expected headline and alternative note, got %q / %q", c.AlertHeadline, c.AlternativeNote)
	}
	if c.ID != "brief-2026-08-29-a" || c.Day != "2026-08-29" {
		t.Errorf("id/day = %q / %q", c.ID, c.Day)
	}
}

func TestDayFor(t *testing.T) {
	loc, _ := time.LoadLocation("America/Sao_Paulo")
	b := &Builder{Loc: loc}
	early := time.Date(2026, 8, 28, 6, 30, 0, 0, loc)
	if got := b.DayKey(b.DayFor(early)); got != "2026-08-28" {
		t.Errorf("06:30 → %s, want today", got)
	}
	late := time.Date(2026, 8, 28, 15, 0, 0, 0, loc)
	if got := b.DayKey(b.DayFor(late)); got != "2026-08-29" {
		t.Errorf("15:00 → %s, want tomorrow", got)
	}
}
