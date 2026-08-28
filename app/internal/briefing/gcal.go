package briefing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"

	"automate-me/app/internal/store"
)

// GoogleCalendarBlocks writes departure blocks to a real Google Calendar
// using Application Default Credentials: on Cloud Run that is the runtime
// service account, which the user grants "make changes to events" on the
// target calendar (no OAuth dance, no service-account keys — the org forbids
// them). Locally: `gcloud auth application-default login --scopes=…calendar`.
type GoogleCalendarBlocks struct {
	svc        *calendar.Service
	calendarID string
}

func NewGoogleCalendarBlocks(ctx context.Context, calendarID string) (*GoogleCalendarBlocks, error) {
	svc, err := calendar.NewService(ctx, option.WithScopes(calendar.CalendarEventsScope))
	if err != nil {
		return nil, fmt.Errorf("calendar service: %w", err)
	}
	// Fail fast on a calendar the identity cannot see.
	if _, err := svc.Calendars.Get(calendarID).Context(ctx).Do(); err != nil {
		return nil, fmt.Errorf("calendar %q not accessible: %w", calendarID, err)
	}
	return &GoogleCalendarBlocks{svc: svc, calendarID: calendarID}, nil
}

// WriteDepartureBlock inserts (or updates, when the card already has a
// google block) a "Leave at HH:MM → event" event spanning the drive.
func (g *GoogleCalendarBlocks) WriteDepartureBlock(ctx context.Context, card store.BriefingCard) (string, string, error) {
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
		updated, err := g.svc.Events.Update(g.calendarID, card.CalendarBlockID, ev).Context(ctx).Do()
		if err == nil {
			return updated.Id, "google", nil
		}
		// fall through to insert if the old event vanished
	}
	created, err := g.svc.Events.Insert(g.calendarID, ev).Context(ctx).Do()
	if err != nil {
		return "", "", fmt.Errorf("calendar insert: %w", err)
	}
	return created.Id, "google", nil
}
