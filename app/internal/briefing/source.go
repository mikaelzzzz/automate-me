package briefing

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Entry is one row of the day exactly as the calendar has it, kind included.
// The briefing prices only the trips, but the SPA shows the whole day: a
// person's agenda is the context the cards live in.
type Entry struct {
	ID       string    `json:"id"`
	Summary  string    `json:"summary"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Kind     string    `json:"kind"` // trip | remote | no_place | ignored
	Location string    `json:"location,omitempty"`
	// Why an entry is not a trip, in the user's words ("Zoom link", "declined").
	Reason string `json:"reason,omitempty"`
}

// Kinds of Entry.
const (
	KindTrip    = "trip"
	KindRemote  = "remote"
	KindNoPlace = "no_place"
	KindIgnored = "ignored"
)

// DaySchedule is a calendar day after the source has separated the trips from
// the screen time. Only Events cost a route call; the counters are what the
// briefing says about everything else, so a fully remote day still reports
// something true instead of an empty page.
type DaySchedule struct {
	Events  []Event `json:"events"`
	Entries []Entry `json:"entries"`  // the whole day, trips included, in time order
	Remote  int     `json:"remote"`   // Zoom/Meet/Teams — nothing to drive to
	NoPlace int     `json:"no_place"` // no location on the event
	Skipped int     `json:"skipped"`  // all-day, declined, out-of-office, our own blocks
	Note    string  `json:"note"`
}

// EventSource supplies the appointments of one day. home is where the user's
// day starts (their profile address); an empty string falls back to whatever
// the source was configured with.
type EventSource interface {
	EventsFor(ctx context.Context, day time.Time, home string) (DaySchedule, error)
	// SourceLabel names where the day came from ("seed", "google:…"), so the
	// UI never leaves the user guessing whose calendar they are reading.
	SourceLabel() string
}

// DemoSource is the seeded São Paulo day: what the briefing shows when no
// calendar is connected (DEMO_MODE=seed, no CALENDAR_ID).
type DemoSource struct{ Loc *time.Location }

func (DemoSource) SourceLabel() string { return "seed" }

func (s DemoSource) EventsFor(_ context.Context, day time.Time, _ string) (DaySchedule, error) {
	evs := DemoAppointments(day, s.Loc)
	entries := make([]Entry, 0, len(evs))
	for _, e := range evs {
		entries = append(entries, Entry{
			ID: e.ID, Summary: e.Summary, Start: e.Start, End: e.Start.Add(time.Hour),
			Kind: KindTrip, Location: e.Destination,
		})
	}
	return DaySchedule{
		Events: evs, Entries: entries,
		Note: fmt.Sprintf("%d seeded appointments — no calendar connected (set CALENDAR_ID to brief your real day).", len(evs)),
	}, nil
}

// conferenceHosts are the locations that are a screen, not a place. A
// location field holding one of these is a meeting link people paste there.
var conferenceHosts = []string{
	"zoom.us", "meet.google.com", "teams.microsoft.com", "teams.live.com",
	"webex.com", "whereby.com", "hangouts.google.com", "chime.aws",
	"gotomeeting.com", "bluejeans.com", "skype.com", "discord.gg", "meet.jit.si",
}

// isRemoteLocation reports whether a calendar location is a meeting link
// rather than an address.
func isRemoteLocation(loc string) bool {
	s := strings.ToLower(strings.TrimSpace(loc))
	if s == "" {
		return false // absent, not remote — the caller counts that separately
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "www.") {
		return true
	}
	for _, h := range conferenceHosts {
		if strings.Contains(s, h) {
			return true
		}
	}
	// A meeting link pasted without its scheme ("httpslyGCVZ…?pwd=x"): query
	// syntax, or one long unbroken token. No street address looks like that.
	if strings.ContainsAny(s, "?=") {
		return true
	}
	if !strings.ContainsAny(s, " ,") && len(s) > 24 {
		return true
	}
	// "Online", "Remoto", "Sala virtual" and friends.
	for _, w := range []string{"online", "remoto", "remote", "virtual", "video call", "videochamada"} {
		if s == w || strings.HasPrefix(s, w+" ") {
			return true
		}
	}
	return false
}

// scheduleNote states the shape of the day in one line: what the agent says
// out loud, and what the SPA prints above the cards.
func (d DaySchedule) withNote(total int, missingHome bool) DaySchedule {
	var parts []string
	if n := len(d.Events); n > 0 {
		parts = append(parts, fmt.Sprintf("%d need%s travel", n, plural2(n)))
	}
	if d.Remote > 0 {
		parts = append(parts, fmt.Sprintf("%d remote", d.Remote))
	}
	if d.NoPlace > 0 {
		parts = append(parts, fmt.Sprintf("%d without a location", d.NoPlace))
	}
	if d.Skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d ignored (all-day, declined or written by me)", d.Skipped))
	}
	switch {
	case total == 0:
		d.Note = "Nothing on the calendar for this day."
	case len(d.Events) == 0:
		d.Note = fmt.Sprintf("%d appointment%s, none of them a trip: %s. No route to price today.",
			total, plural(total), strings.Join(parts, ", "))
	default:
		d.Note = fmt.Sprintf("%d appointment%s: %s.", total, plural(total), strings.Join(parts, ", "))
	}
	if missingHome {
		d.Note += " Some trips had no known origin — set HOME_ADDRESS to route them from home."
	}
	return d
}

func plural2(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}

// Schedule reads the day from the connected source. A nil source is the
// seeded day; a failing calendar is an error, never silently seeded data —
// the briefing would otherwise brief someone else's life.
func (b *Builder) Schedule(ctx context.Context, src EventSource, day time.Time, home string) (DaySchedule, error) {
	if src == nil {
		src = DemoSource{Loc: b.Loc}
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return src.EventsFor(ctx, day, home)
}
