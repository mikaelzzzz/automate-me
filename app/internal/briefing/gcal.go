package briefing

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"automate-me/app/internal/store"
)

// GoogleCalendar is both ends of the calendar connection: it reads the day's
// real appointments (EventSource) and writes the departure blocks back
// (BlockWriter), over one authenticated service.
//
// Auth is Application Default Credentials: on Cloud Run that is the runtime
// service account, which the user grants "make changes to events" on the
// target calendar (no OAuth dance, no service-account keys — the org forbids
// them). Locally: `gcloud auth application-default login --scopes=…calendar`.
type GoogleCalendar struct {
	svc *calendar.Service
	// write goes to the first calendar; read spans all of them (a personal
	// calendar usually holds the appointments that involve travel).
	writeID string
	readIDs []string
	loc     *time.Location
	// Home is the origin for the first trip of the day (HOME_ADDRESS).
	Home string
	// MaxTrips bounds the Routes API calls one briefing can make.
	MaxTrips int
}

// NewGoogleCalendar opens the calendars named by CALENDAR_ID (comma-separated;
// the first one is where blocks are written) and fails fast on any the
// identity cannot see.
func NewGoogleCalendar(ctx context.Context, calendarIDs string, loc *time.Location, home string) (*GoogleCalendar, error) {
	var ids []string
	for _, id := range strings.Split(calendarIDs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no calendar id given")
	}
	svc, err := calendar.NewService(ctx, option.WithScopes(calendar.CalendarEventsScope))
	if err != nil {
		return nil, fmt.Errorf("calendar service: %w", err)
	}
	for _, id := range ids {
		if _, err := svc.Calendars.Get(id).Context(ctx).Do(); err != nil {
			return nil, fmt.Errorf("calendar %q not accessible: %w", id, err)
		}
	}
	return &GoogleCalendar{svc: svc, writeID: ids[0], readIDs: ids, loc: loc, Home: strings.TrimSpace(home), MaxTrips: 6}, nil
}

// Calendars lists the calendars this connection reads, first one written to.
func (g *GoogleCalendar) Calendars() []string { return g.readIDs }

// EventsFor reads one local day across every connected calendar and splits it
// into trips, screen time, and noise. Only the trips cost a route call.
func (g *GoogleCalendar) EventsFor(ctx context.Context, day time.Time) (DaySchedule, error) {
	local := day.In(g.loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, g.loc)
	to := from.AddDate(0, 0, 1)

	var raw []*calendar.Event
	for _, id := range g.readIDs {
		items, err := g.svc.Events.List(id).
			SingleEvents(true).OrderBy("startTime").
			TimeMin(from.Format(time.RFC3339)).TimeMax(to.Format(time.RFC3339)).
			MaxResults(100).Context(ctx).Do()
		if err != nil {
			return DaySchedule{}, fmt.Errorf("list events on %q: %w", id, err)
		}
		raw = append(raw, items.Items...)
	}
	return g.classify(raw), nil
}

// classify turns raw calendar events into a DaySchedule: every row of the day
// kept as an Entry, and the subset worth a route call as Events. Pure given
// its input — the API call is the only thing above it.
func (g *GoogleCalendar) classify(raw []*calendar.Event) DaySchedule {
	type dated struct {
		ev    *calendar.Event
		start time.Time
		end   time.Time
	}
	var day DaySchedule
	var items []dated
	total := 0

	for _, ev := range raw {
		if ev == nil || ev.Start == nil || ev.Start.DateTime == "" {
			continue // all-day events and malformed rows are not appointments
		}
		start, err := time.Parse(time.RFC3339, ev.Start.DateTime)
		if err != nil {
			continue
		}
		total++
		end := start.Add(time.Hour)
		if ev.End != nil && ev.End.DateTime != "" {
			if e, err := time.Parse(time.RFC3339, ev.End.DateTime); err == nil {
				end = e
			}
		}
		items = append(items, dated{ev: ev, start: start.In(g.loc), end: end.In(g.loc)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].start.Before(items[j].start) })

	var lastPlace string
	var lastEnd time.Time
	missingHome := false
	for _, it := range items {
		entry := Entry{
			ID: eventKey(it.ev), Summary: strings.TrimSpace(it.ev.Summary),
			Start: it.start, End: it.end, Location: strings.TrimSpace(it.ev.Location),
		}
		if reason := skipReason(it.ev); reason != "" {
			day.Skipped++
			entry.Kind, entry.Reason = KindIgnored, reason
			entry.Location = ""
			day.Entries = append(day.Entries, entry)
			continue
		}
		loc := entry.Location
		switch {
		case it.ev.ConferenceData != nil || it.ev.HangoutLink != "" || isRemoteLocation(loc):
			day.Remote++
			entry.Kind, entry.Reason = KindRemote, "meeting link, not a place"
			day.Entries = append(day.Entries, entry)
			continue
		case loc == "":
			day.NoPlace++
			entry.Kind, entry.Reason = KindNoPlace, "no location on the event"
			day.Entries = append(day.Entries, entry)
			continue
		}
		// Same place as the appointment before it: no trip between them.
		if strings.EqualFold(loc, lastPlace) {
			lastEnd = it.end
			entry.Kind, entry.Reason = KindNoPlace, "same place as the appointment before it"
			day.NoPlace++
			day.Entries = append(day.Entries, entry)
			continue
		}
		origin := g.Home
		// Chained trip: coming straight from the previous appointment, if it
		// ends within four hours of this one starting.
		if lastPlace != "" && !lastEnd.IsZero() && it.start.Sub(lastEnd) <= 4*time.Hour {
			origin = lastPlace
		}
		if origin == "" {
			missingHome = true
			day.NoPlace++
			entry.Kind, entry.Reason = KindNoPlace, "no origin to route from (set HOME_ADDRESS)"
			day.Entries = append(day.Entries, entry)
			continue
		}
		if len(day.Events) >= max(g.MaxTrips, 1) {
			day.Skipped++
			entry.Kind, entry.Reason = KindIgnored, "over the daily route budget"
			day.Entries = append(day.Entries, entry)
			continue
		}
		day.Events = append(day.Events, Event{
			ID: entry.ID, Summary: entry.Summary, Start: it.start,
			Origin: origin, Destination: loc,
		})
		entry.Kind = KindTrip
		day.Entries = append(day.Entries, entry)
		lastPlace, lastEnd = loc, it.end
	}
	return day.withNote(total, missingHome)
}

// skipReason says why a row on the calendar is not an appointment to travel
// to — and stays empty when it is. Out-of-office and working-location rows,
// cancelled events, invitations the user declined, and the departure blocks
// this app wrote itself (reading those back would brief a trip to a trip).
func skipReason(ev *calendar.Event) string {
	if ev.Status == "cancelled" {
		return "cancelled"
	}
	switch ev.EventType {
	case "outOfOffice":
		return "out of office"
	case "workingLocation":
		return "working location"
	case "birthday":
		return "birthday"
	case "focusTime":
		return "focus time"
	}
	if ev.ExtendedProperties != nil && ev.ExtendedProperties.Private["automate_me_kind"] != "" {
		return "written by Automate.me"
	}
	for _, a := range ev.Attendees {
		if a.Self && a.ResponseStatus == "declined" {
			return "you declined it"
		}
	}
	return ""
}

// SourceLabel names the calendars this day was read from.
func (g *GoogleCalendar) SourceLabel() string { return "google:" + strings.Join(g.readIDs, ",") }

// eventKey is a stable, filename-safe id for a card built from this event.
func eventKey(ev *calendar.Event) string {
	id := ev.Id
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(ev.Summary, " ", "-"))
	}
	if len(id) > 40 {
		id = id[:40]
	}
	return id
}

// WriteDepartureBlock inserts (or updates, when the card already has a
// google block) a "Leave at HH:MM → event" event spanning the drive.
func (g *GoogleCalendar) WriteDepartureBlock(ctx context.Context, card store.BriefingCard) (string, string, error) {
	loc := card.EventStart.Location()
	dep := card.DepartureTime.In(loc)
	end := card.EventStart.In(loc)
	if end.Before(dep.Add(5 * time.Minute)) {
		end = dep.Add(time.Duration(max(card.RouteMinutes, 5)) * time.Minute)
	}
	var desc []string
	desc = append(desc, card.RouteSummary)
	if card.TrafficMinutes > 0 {
		desc = append(desc, fmt.Sprintf("Traffic adds %d min (R$%d.%02d at your rate).", card.TrafficMinutes, card.TrafficCents/100, card.TrafficCents%100))
	}
	if card.Weather != "" {
		desc = append(desc, "Weather at departure: "+card.Weather+". Wear "+card.Clothing+".")
	}
	if card.FloodRisk != "none" {
		desc = append(desc, "Flood: "+card.FloodDetail+".")
	}
	if card.AlternativeNote != "" {
		desc = append(desc, card.AlternativeNote)
	}
	desc = append(desc, "", "Written by Automate.me · Daily Briefing")

	ev := &calendar.Event{
		Summary:     fmt.Sprintf("Leave at %s → %s", dep.Format("15:04"), card.EventSummary),
		Description: strings.Join(desc, "\n"),
		Location:    card.Destination,
		Start:       &calendar.EventDateTime{DateTime: dep.Format(time.RFC3339), TimeZone: loc.String()},
		End:         &calendar.EventDateTime{DateTime: end.Format(time.RFC3339), TimeZone: loc.String()},
		ColorId:     "5", // banana — the sun accent
		Reminders:   &calendar.EventReminders{UseDefault: false, Overrides: []*calendar.EventReminder{{Method: "popup", Minutes: 10}}, ForceSendFields: []string{"UseDefault"}},
		ExtendedProperties: &calendar.EventExtendedProperties{
			Private: map[string]string{"automate_me_card": card.ID, "automate_me_kind": "departure_block"},
		},
	}
	if card.CalendarBlockMode == "google" && card.CalendarBlockID != "" {
		updated, err := g.svc.Events.Update(g.writeID, card.CalendarBlockID, ev).Context(ctx).Do()
		if err == nil {
			return updated.Id, "google", nil
		}
		// fall through to insert if the old event vanished
	}
	created, err := g.svc.Events.Insert(g.writeID, ev).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("calendar insert: %w", err)
	}
	return created.Id, "google", nil
}
