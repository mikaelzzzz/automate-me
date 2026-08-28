// Package briefing builds the Daily Briefing (PRD F5): for each appointment
// of the day, one route worker computes the calmest departure time
// (Routes API, future departureTime), prices the congestion with the Value
// Engine's rate (duration − staticDuration × hourly rate), reads the hourly
// forecast at departure (Weather API), and raises flood risk from two layers:
// live public alerts whose polygon the route crosses, and GeoSampa's historic
// flooding points within 150 m of the route. Deterministic Go; the LLM only
// narrates the cards.
package briefing

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"automate-me/app/internal/store"
)

// Event is one appointment the user must travel to.
type Event struct {
	ID          string    `json:"id"`
	Summary     string    `json:"summary"`
	Start       time.Time `json:"start"`
	Origin      string    `json:"origin"`
	Destination string    `json:"destination"`
	// Optional precise coordinates (calendar events carry addresses; the
	// demo pins two destinations onto logged flooding clusters).
	OriginLatLng *LatLng `json:"origin_latlng,omitempty"`
	DestLatLng   *LatLng `json:"dest_latlng,omitempty"`
}

// Place is an address or a coordinate for the Routes API.
type Place struct {
	Address string
	LatLng  *LatLng
}

// mapsAPI is what Build needs from Google Maps Platform (MapsClient in prod,
// a fake in tests).
type mapsAPI interface {
	ComputeRoute(ctx context.Context, origin, destination Place, departure time.Time) (Route, error)
	HourlyForecast(ctx context.Context, p LatLng, at time.Time) (Weather, error)
	PublicAlerts(ctx context.Context, p LatLng) ([]Alert, error)
}

// Builder fans one worker out per event (bounded), each with its own
// context deadline; a failing API degrades that card, never the briefing.
type Builder struct {
	Maps        mapsAPI
	Now         func() time.Time
	Loc         *time.Location
	Parallelism int
	// FloodRadiusM: historic points closer than this to the route count.
	FloodRadiusM float64
	// ArriveEarly is the buffer before the appointment.
	ArriveEarly time.Duration
}

func NewBuilder(m *MapsClient, loc *time.Location) *Builder {
	return &Builder{Maps: m, Now: time.Now, Loc: loc, Parallelism: 4, FloodRadiusM: 150, ArriveEarly: 5 * time.Minute}
}

// DayFor picks the day being briefed: today before 08:00 local, else tomorrow.
func (b *Builder) DayFor(now time.Time) time.Time {
	local := now.In(b.Loc)
	d := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, b.Loc)
	if local.Hour() >= 8 {
		d = d.AddDate(0, 0, 1)
	}
	return d
}

// DayKey formats a day as YYYY-MM-DD in the briefing timezone.
func (b *Builder) DayKey(day time.Time) string { return day.In(b.Loc).Format("2006-01-02") }

// Build produces one card per event, in input order.
func (b *Builder) Build(ctx context.Context, userID string, hourlyRateCents int64, events []Event) []store.BriefingCard {
	cards := make([]store.BriefingCard, len(events))
	var mu sync.Mutex
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(max(b.Parallelism, 1))
	for i, ev := range events {
		g.Go(func() error {
			cctx, cancel := context.WithTimeout(gctx, 25*time.Second)
			defer cancel()
			card := b.buildOne(cctx, userID, hourlyRateCents, ev)
			mu.Lock()
			cards[i] = card
			mu.Unlock()
			return nil // partial cards beat no briefing
		})
	}
	_ = g.Wait()
	return cards
}

func (b *Builder) buildOne(ctx context.Context, userID string, rate int64, ev Event) store.BriefingCard {
	day := b.DayKey(ev.Start)
	card := store.BriefingCard{
		ID: "brief-" + day + "-" + ev.ID, UserID: userID, Day: day,
		EventSummary: ev.Summary, EventStart: ev.Start,
		Origin: ev.Origin, Destination: ev.Destination,
		FloodRisk: "none",
	}

	// Pass 1: rough departure; pass 2: refined with pass-1 duration. Traffic
	// changes with departure time, so the second call is the honest one.
	from := Place{Address: ev.Origin, LatLng: ev.OriginLatLng}
	to := Place{Address: ev.Destination, LatLng: ev.DestLatLng}
	dep := ev.Start.Add(-40 * time.Minute)
	r, err := b.Maps.ComputeRoute(ctx, from, to, dep)
	if err != nil {
		card.RouteSummary = "route unavailable: " + trimErr(err)
		card.DepartureTime = ev.Start.Add(-45 * time.Minute)
		card.Notes = append(card.Notes, "Routes API failed; departure is a 45-minute guess.")
		return card
	}
	dep = ev.Start.Add(-b.ArriveEarly - r.Duration)
	if r2, err := b.Maps.ComputeRoute(ctx, from, to, dep); err == nil {
		r = r2
		dep = ev.Start.Add(-b.ArriveEarly - r.Duration)
	}
	traffic := r.Duration - r.StaticDuration
	if traffic < 0 {
		traffic = 0
	}
	card.DepartureTime = dep.Round(time.Minute)
	card.RouteMinutes = int(math.Round(r.Duration.Minutes()))
	card.TrafficMinutes = int(math.Round(traffic.Minutes()))
	card.TrafficCents = int64(math.Round(traffic.Hours() * float64(rate)))
	card.RouteSummary = fmt.Sprintf("%d min via %s · %.1f km", card.RouteMinutes, nonEmpty(r.Description, "the fastest route"), float64(r.DistanceMetres)/1000)

	// Weather at departure, from the origin.
	var heat bool
	if len(r.Path) > 0 {
		if w, err := b.Maps.HourlyForecast(ctx, r.Path[0], dep); err == nil {
			card.Weather = fmt.Sprintf("%.0f°C, %s, %d%% chance of rain", w.TempC, strings.ToLower(nonEmpty(w.Condition, "no forecast")), w.RainChancePct)
			card.WeatherTempC = w.TempC
			card.RainChancePct = w.RainChancePct
			card.Clothing = clothingFor(w)
		} else {
			card.Notes = append(card.Notes, "Weather API failed; no forecast for this trip.")
		}
	}

	// Flood layer 1: live alerts whose polygon the route crosses.
	if len(r.Path) > 0 {
		if alerts, err := b.Maps.PublicAlerts(ctx, r.Path[len(r.Path)-1]); err == nil {
			for _, a := range alerts {
				crosses := len(a.polys) == 0 || pathCrosses(r.Path, a.polys)
				if a.IsFlood() && crosses {
					card.FloodRisk = "alert"
					card.FloodDetail = fmt.Sprintf("%s alert (%s) on this route: %s", strings.ReplaceAll(a.EventType, "_", " "), strings.ToLower(a.Severity), firstSentence(a.Description))
				} else if a.EventType == "HEAT" && crosses {
					heat = true
				}
				if card.AlertHeadline == "" && (a.Severity == "SEVERE" || a.Severity == "EXTREME") && crosses {
					card.AlertHeadline = fmt.Sprintf("%s · %s", nonEmpty(a.Title, a.EventType), strings.ToLower(a.Severity))
				}
			}
		} else {
			card.Notes = append(card.Notes, "Public alerts unavailable.")
		}
	}
	if heat && card.Clothing != "" {
		card.Clothing += " · heat alert: carry water"
	}

	// Flood layer 2: GeoSampa historic occurrences near the route.
	if pts := FloodPointsNear(r.Path, b.FloodRadiusM); len(pts) > 0 {
		card.FloodPoints = len(pts)
		if card.FloodRisk == "none" {
			card.FloodRisk = "historic"
		}
		areas := Subprefectures(pts)
		if len(areas) > 3 {
			areas = areas[:3]
		}
		detail := fmt.Sprintf("route crosses %d point%s with flooding history", len(pts), plural(len(pts)))
		if len(areas) > 0 {
			detail += " (" + strings.Join(areas, ", ") + ")"
		}
		if card.FloodDetail == "" {
			card.FloodDetail = detail
		} else {
			card.FloodDetail += "; " + detail
		}
	}

	// Leave-on-time recipe: when the trip itself is the leak, say so.
	switch {
	case card.FloodRisk == "alert":
		card.AlternativeNote = "Flood alert on the route — take the video-call option or leave 20 min earlier."
	case card.TrafficMinutes >= 15:
		card.AlternativeNote = fmt.Sprintf("Traffic adds %d min (R$%d.%02d) to this trip — a video call would save it.", card.TrafficMinutes, card.TrafficCents/100, card.TrafficCents%100)
	case card.FloodRisk == "historic" && card.RainChancePct >= 40:
		card.AlternativeNote = "Rain likely over a route with flooding history — leave 15 min earlier or go by metro."
	}
	return card
}

func clothingFor(w Weather) string {
	var c string
	switch {
	case w.TempC >= 28:
		c = "light clothes, sunscreen"
	case w.TempC >= 21:
		c = "light layers"
	case w.TempC >= 15:
		c = "a jacket"
	default:
		c = "a coat"
	}
	if w.RainChancePct >= 40 {
		c += ", umbrella"
	}
	return c
}

// DemoAppointments is the seeded calendar for the demo user (São Paulo).
// Two destinations are pinned to the centroids of GeoSampa flooding
// clusters (Vila Prudente: 13 logged floodings, Aricanduva/Vila Formosa: 9)
// so the historic layer has something real to say on those routes.
func DemoAppointments(day time.Time, loc *time.Location) []Event {
	at := func(h, m int) time.Time {
		d := day.In(loc)
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc)
	}
	office := "Av. Paulista 1578, Bela Vista, São Paulo"
	return []Event{
		{ID: "client", Summary: "Client meeting · Av. Paulista", Start: at(9, 0),
			Origin: "Rua dos Pinheiros 1000, Pinheiros, São Paulo", Destination: office},
		{ID: "pediatrician", Summary: "Pediatrician · Vila Prudente", Start: at(14, 30),
			Origin: office, Destination: "Vila Prudente, São Paulo", DestLatLng: &LatLng{Lat: -23.61242, Lng: -46.52891}},
		{ID: "judo", Summary: "Kids' judo class · Vila Formosa", Start: at(18, 30),
			Origin: office, Destination: "Vila Formosa, São Paulo", DestLatLng: &LatLng{Lat: -23.55011, Lng: -46.54716}},
	}
}

// BlockWriter writes "Leave at HH:MM → event" blocks to a calendar.
type BlockWriter interface {
	WriteDepartureBlock(ctx context.Context, card store.BriefingCard) (id, mode string, err error)
}

// SimulatedBlocks is the degraded writer: no calendar connected, the block is
// recorded in-app and labelled simulated.
type SimulatedBlocks struct{}

func (SimulatedBlocks) WriteDepartureBlock(_ context.Context, card store.BriefingCard) (string, string, error) {
	return "sim-" + card.ID, "simulated", nil
}

func nonEmpty(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func firstSentence(s string) string {
	if i := strings.IndexAny(s, ".!"); i > 0 && i < 140 {
		return s[:i+1]
	}
	if len(s) > 140 {
		return s[:140] + "…"
	}
	return s
}

func trimErr(err error) string {
	s := err.Error()
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
