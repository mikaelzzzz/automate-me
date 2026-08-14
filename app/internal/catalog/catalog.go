// Package catalog holds the automation recipe catalog. Recipes are data
// (PRD §5.2/§5.3): adding one is a seed change, not code. Matching pairs a
// declared routine task with candidate recipes; the Value Engine decides if
// they are worth proposing.
package catalog

import "strings"

// Capability is the canonical executor enum (design §4 — single source of
// truth, mirrored by PRD F4).
type Capability string

const (
	CapVision        Capability = "vision"
	CapCalendarWrite Capability = "calendar_write"
	CapMapsRoutes    Capability = "maps_routes"
	CapWeatherFlood  Capability = "weather_flood"
	CapGmailDraft    Capability = "gmail_draft"
	CapAP2Purchase   Capability = "ap2_purchase"
	CapReportGen     Capability = "report_gen"
)

// Class distinguishes how a recipe acts.
type Class string

const (
	ClassExecutable Class = "executable" // agent performs it
	ClassAdvised    Class = "advised"    // payback card + steps for the user
	ClassRoadmap    Class = "roadmap"    // listed to show vision; not built
)

// CostModel prices an automation (minor units, BRL).
type CostModel struct {
	UpfrontCents        int64
	MonthlyRunningCents int64
	// MinutesSavedPerOcc: minutes recovered each time the routine would occur.
	MinutesSavedPerOcc int
}

// Recipe is one catalog entry.
type Recipe struct {
	ID          string
	Title       string
	Description string
	Class       Class
	Capability  Capability
	Cost        CostModel
	// Triggers are lowercase keywords matched against the task name/notes
	// (pt-BR and en — users declare in either language).
	Triggers []string
	// ProductID links executable AP2 recipes to the merchant catalog.
	ProductID string
}

// Match returns recipes whose triggers appear in the task text, executables
// first (stable order within class follows seed order).
func Match(taskText string, recipes []Recipe) []Recipe {
	text := strings.ToLower(taskText)
	var exec, rest []Recipe
	for _, r := range recipes {
		for _, trig := range r.Triggers {
			if strings.Contains(text, trig) {
				if r.Class == ClassExecutable {
					exec = append(exec, r)
				} else {
					rest = append(rest, r)
				}
				break
			}
		}
	}
	return append(exec, rest...)
}
