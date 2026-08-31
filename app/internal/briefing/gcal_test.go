package briefing

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/api/calendar/v3"
)

func spTime(t *testing.T, s string) string {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts.Format(time.RFC3339)
}

func ev(t *testing.T, summary, location, start, end string) *calendar.Event {
	t.Helper()
	return &calendar.Event{
		Id: summary, Summary: summary, Location: location, Status: "confirmed",
		Start: &calendar.EventDateTime{DateTime: spTime(t, start)},
		End:   &calendar.EventDateTime{DateTime: spTime(t, end)},
	}
}

// classifyDefault classifies with the calendar's own home address, which is
// what the server does when the user's profile carries no address.
func (g *GoogleCalendar) classifyDefault(raw []*calendar.Event) DaySchedule {
	return g.classify(raw, g.Home)
}

func testCalendar(t *testing.T, home string) *GoogleCalendar {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	return &GoogleCalendar{loc: loc, Home: home, MaxTrips: 6}
}

// A day of Zoom classes is a real day: no routes, but the briefing must say
// what it saw instead of showing an empty page.
func TestClassifyRemoteDay(t *testing.T) {
	g := testCalendar(t, "Rua dos Pinheiros 1000, São Paulo")
	got := g.classifyDefault([]*calendar.Event{
		ev(t, "Madrugas - 8 AM", "https://us06web.zoom.us/j/84961898183?pwd=x", "2026-09-01 08:00", "2026-09-01 09:00"),
		ev(t, "Coldplay", "https://meet.google.com/qzm-cmtt-eax", "2026-09-01 15:00", "2026-09-01 16:00"),
		ev(t, "Terapia", "", "2026-09-01 10:00", "2026-09-01 11:00"),
	})
	if len(got.Events) != 0 {
		t.Fatalf("want no trips, got %d", len(got.Events))
	}
	if got.Remote != 2 || got.NoPlace != 1 {
		t.Fatalf("want remote=2 no_place=1, got remote=%d no_place=%d", got.Remote, got.NoPlace)
	}
	if got.Note == "" {
		t.Fatal("want a note describing the day")
	}
}

func TestClassifyOriginChaining(t *testing.T) {
	home := "Rua dos Pinheiros 1000, São Paulo"
	g := testCalendar(t, home)
	got := g.classifyDefault([]*calendar.Event{
		ev(t, "Terapia", "Av. Paulista 1578, São Paulo", "2026-09-01 10:00", "2026-09-01 11:00"),
		// Same morning, straight from the last address.
		ev(t, "Pediatra", "Vila Prudente, São Paulo", "2026-09-01 12:00", "2026-09-01 13:00"),
		// Eight hours later: back home first.
		ev(t, "Judô", "Vila Formosa, São Paulo", "2026-09-01 21:00", "2026-09-01 22:00"),
	})
	if len(got.Events) != 3 {
		t.Fatalf("want 3 trips, got %d (%s)", len(got.Events), got.Note)
	}
	want := []string{home, "Av. Paulista 1578, São Paulo", home}
	for i, w := range want {
		if got.Events[i].Origin != w {
			t.Errorf("trip %d origin = %q, want %q", i, got.Events[i].Origin, w)
		}
	}
}

// Reading our own departure blocks back would brief a trip to a trip.
func TestClassifySkipsOwnBlocksAndNoise(t *testing.T) {
	g := testCalendar(t, "home")
	block := ev(t, "Leave at 09:20 → Terapia", "Av. Paulista 1578", "2026-09-01 09:20", "2026-09-01 10:00")
	block.ExtendedProperties = &calendar.EventExtendedProperties{
		Private: map[string]string{"automate_me_kind": "departure_block"},
	}
	ooo := ev(t, "Almoço", "Rua Augusta 100", "2026-09-01 12:00", "2026-09-01 13:00")
	ooo.EventType = "outOfOffice"
	declined := ev(t, "Reunião", "Av. Faria Lima 500", "2026-09-01 14:00", "2026-09-01 15:00")
	declined.Attendees = []*calendar.EventAttendee{{Self: true, ResponseStatus: "declined"}}
	allDay := &calendar.Event{Id: "holiday", Summary: "Feriado", Status: "confirmed",
		Start: &calendar.EventDateTime{Date: "2026-09-01"}, End: &calendar.EventDateTime{Date: "2026-09-02"}}

	got := g.classifyDefault([]*calendar.Event{block, ooo, declined, allDay,
		ev(t, "Terapia", "Av. Paulista 1578", "2026-09-01 10:00", "2026-09-01 11:00")})

	if len(got.Events) != 1 || got.Events[0].Summary != "Terapia" {
		t.Fatalf("want only the real appointment, got %+v", got.Events)
	}
	if got.Skipped != 3 {
		t.Errorf("skipped = %d, want 3 (block, out-of-office, declined)", got.Skipped)
	}
}

// Two appointments at the same address in a row are one trip.
func TestClassifyCollapsesSamePlace(t *testing.T) {
	g := testCalendar(t, "home")
	got := g.classifyDefault([]*calendar.Event{
		ev(t, "Aula 1", "Av. Paulista 1578", "2026-09-01 10:00", "2026-09-01 11:00"),
		ev(t, "Aula 2", "av. paulista 1578", "2026-09-01 11:00", "2026-09-01 12:00"),
	})
	if len(got.Events) != 1 {
		t.Fatalf("want 1 trip, got %d", len(got.Events))
	}
}

// No home address and no previous stop: nothing to route from, and the note
// has to say why.
func TestClassifyWithoutHomeAddress(t *testing.T) {
	g := testCalendar(t, "")
	got := g.classifyDefault([]*calendar.Event{
		ev(t, "Terapia", "Av. Paulista 1578", "2026-09-01 10:00", "2026-09-01 11:00"),
	})
	if len(got.Events) != 0 {
		t.Fatalf("want no trips without an origin, got %d", len(got.Events))
	}
	if !strings.Contains(got.Note, "HOME_ADDRESS") {
		t.Errorf("note should name HOME_ADDRESS, got %q", got.Note)
	}
}

func TestIsRemoteLocation(t *testing.T) {
	remote := []string{
		"https://us06web.zoom.us/j/84961898183?pwd=x",
		" https://us06web.zoom.us/j/88011712419",
		"https://meet.google.com/qzm-cmtt-eax",
		"teams.microsoft.com/l/meetup-join/x",
		"httpslyGCVZJ9mlrWqkUML5GlQbsL7HkP3k.1?pwd=lyGCVZJ9mlrWqkUML5GlQbsL7HkP3k.1", // link pasted without its scheme
		"Online",
		"Remoto — sala 2",
	}
	for _, s := range remote {
		if !isRemoteLocation(s) {
			t.Errorf("isRemoteLocation(%q) = false, want true", s)
		}
	}
	physical := []string{"Av. Paulista 1578, Bela Vista", "Vila Prudente, São Paulo", "Rua Zoom 40", "Consultorio Dra Marina"}
	for _, s := range physical {
		if isRemoteLocation(s) {
			t.Errorf("isRemoteLocation(%q) = true, want false", s)
		}
	}
	if isRemoteLocation("") {
		t.Error("empty location is absent, not remote")
	}
}
