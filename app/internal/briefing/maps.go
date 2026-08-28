package briefing

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Google Maps Platform clients: Routes API (traffic-aware, future departure)
// and Weather API (hourly forecast + public alerts). Plain HTTP, one API key,
// context timeouts — the Briefing renders partial cards on failure.

type Route struct {
	Duration       time.Duration `json:"duration"`
	StaticDuration time.Duration `json:"static_duration"`
	DistanceMetres int           `json:"distance_metres"`
	Description    string        `json:"description"`
	Path           []LatLng      `json:"-"`
}

type Weather struct {
	At            time.Time `json:"at"`
	TempC         float64   `json:"temp_c"`
	Condition     string    `json:"condition"`
	RainChancePct int       `json:"rain_chance_pct"`
}

type Alert struct {
	ID          string    `json:"id"`
	EventType   string    `json:"event_type"` // HEAT, FLOOD, FLASH_FLOOD, THUNDERSTORM…
	Severity    string    `json:"severity"`   // MINOR | MODERATE | SEVERE | EXTREME
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Expires     time.Time `json:"expires"`
	polys       []polygon
}

func (a Alert) IsFlood() bool {
	return strings.Contains(a.EventType, "FLOOD")
}

// MapsClient talks to Routes + Weather with one key.
type MapsClient struct {
	Key  string
	HTTP *http.Client
}

func NewMapsClient(key string) *MapsClient {
	return &MapsClient{Key: key, HTTP: &http.Client{Timeout: 12 * time.Second}}
}

// ComputeRoute: driving, traffic-aware, for a future departure. FieldMask
// keeps the response tiny.
func (c *MapsClient) ComputeRoute(ctx context.Context, origin, destination Place, departure time.Time) (Route, error) {
	body := map[string]any{
		"origin":            origin.waypoint(),
		"destination":       destination.waypoint(),
		"travelMode":        "DRIVE",
		"routingPreference": "TRAFFIC_AWARE_OPTIMAL",
		"departureTime":     departure.UTC().Format(time.RFC3339),
		"languageCode":      "en",
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://routes.googleapis.com/directions/v2:computeRoutes", bytes.NewReader(raw))
	if err != nil {
		return Route{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", c.Key)
	req.Header.Set("X-Goog-FieldMask", "routes.duration,routes.staticDuration,routes.distanceMeters,routes.polyline.encodedPolyline,routes.description")
	var out struct {
		Routes []struct {
			Duration       string `json:"duration"`
			StaticDuration string `json:"staticDuration"`
			DistanceMeters int    `json:"distanceMeters"`
			Description    string `json:"description"`
			Polyline       struct {
				Encoded string `json:"encodedPolyline"`
			} `json:"polyline"`
		} `json:"routes"`
		Error *apiError `json:"error"`
	}
	if err := c.do(req, &out); err != nil {
		return Route{}, err
	}
	if out.Error != nil {
		return Route{}, fmt.Errorf("routes api: %s", out.Error.Message)
	}
	if len(out.Routes) == 0 {
		return Route{}, fmt.Errorf("routes api: no route from %q to %q", origin.Address, destination.Address)
	}
	r := out.Routes[0]
	return Route{
		Duration:       parseSeconds(r.Duration),
		StaticDuration: parseSeconds(r.StaticDuration),
		DistanceMetres: r.DistanceMeters,
		Description:    r.Description,
		Path:           DecodePolyline(r.Polyline.Encoded),
	}, nil
}

// HourlyForecast returns the forecast hour covering `at` (falls back to the
// first hour available).
func (c *MapsClient) HourlyForecast(ctx context.Context, p LatLng, at time.Time) (Weather, error) {
	q := url.Values{}
	q.Set("key", c.Key)
	q.Set("location.latitude", strconv.FormatFloat(p.Lat, 'f', 5, 64))
	q.Set("location.longitude", strconv.FormatFloat(p.Lng, 'f', 5, 64))
	q.Set("hours", "48")
	q.Set("languageCode", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://weather.googleapis.com/v1/forecast/hours:lookup?"+q.Encode(), nil)
	if err != nil {
		return Weather{}, err
	}
	var out struct {
		ForecastHours []struct {
			Interval struct {
				StartTime time.Time `json:"startTime"`
				EndTime   time.Time `json:"endTime"`
			} `json:"interval"`
			Temperature struct {
				Degrees float64 `json:"degrees"`
			} `json:"temperature"`
			WeatherCondition struct {
				Description struct {
					Text string `json:"text"`
				} `json:"description"`
			} `json:"weatherCondition"`
			Precipitation struct {
				Probability struct {
					Percent int `json:"percent"`
				} `json:"probability"`
			} `json:"precipitation"`
		} `json:"forecastHours"`
		Error *apiError `json:"error"`
	}
	if err := c.do(req, &out); err != nil {
		return Weather{}, err
	}
	if out.Error != nil {
		return Weather{}, fmt.Errorf("weather api: %s", out.Error.Message)
	}
	if len(out.ForecastHours) == 0 {
		return Weather{}, fmt.Errorf("weather api: empty forecast")
	}
	pick := out.ForecastHours[0]
	for _, h := range out.ForecastHours {
		if !at.Before(h.Interval.StartTime) && at.Before(h.Interval.EndTime) {
			pick = h
			break
		}
	}
	return Weather{
		At:            pick.Interval.StartTime,
		TempC:         pick.Temperature.Degrees,
		Condition:     pick.WeatherCondition.Description.Text,
		RainChancePct: pick.Precipitation.Probability.Percent,
	}, nil
}

// PublicAlerts returns active government alerts around p, polygons parsed.
func (c *MapsClient) PublicAlerts(ctx context.Context, p LatLng) ([]Alert, error) {
	q := url.Values{}
	q.Set("key", c.Key)
	q.Set("location.latitude", strconv.FormatFloat(p.Lat, 'f', 5, 64))
	q.Set("location.longitude", strconv.FormatFloat(p.Lng, 'f', 5, 64))
	q.Set("languageCode", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://weather.googleapis.com/v1/publicAlerts:lookup?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		WeatherAlerts []struct {
			AlertID    string `json:"alertId"`
			AlertTitle struct {
				Text string `json:"text"`
			} `json:"alertTitle"`
			EventType      string    `json:"eventType"`
			Severity       string    `json:"severity"`
			Description    string    `json:"description"`
			Polygon        string    `json:"polygon"`
			ExpirationTime time.Time `json:"expirationTime"`
		} `json:"weatherAlerts"`
		Error *apiError `json:"error"`
	}
	if err := c.do(req, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("weather alerts api: %s", out.Error.Message)
	}
	alerts := make([]Alert, 0, len(out.WeatherAlerts))
	for _, a := range out.WeatherAlerts {
		al := Alert{
			ID: a.AlertID, EventType: a.EventType, Severity: a.Severity,
			Title: a.AlertTitle.Text, Description: a.Description, Expires: a.ExpirationTime,
		}
		if a.Polygon != "" {
			al.polys, _ = parseMultiPolygon(json.RawMessage(a.Polygon))
		}
		alerts = append(alerts, al)
	}
	return alerts, nil
}

func (p Place) waypoint() map[string]any {
	if p.LatLng != nil {
		return map[string]any{"location": map[string]any{"latLng": map[string]any{"latitude": p.LatLng.Lat, "longitude": p.LatLng.Lng}}}
	}
	return map[string]any{"address": p.Address}
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *MapsClient) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: status %d: %w", req.URL.Host, resp.StatusCode, err)
	}
	return nil
}

// "847s" → 847s
func parseSeconds(s string) time.Duration {
	s = strings.TrimSuffix(s, "s")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return time.Duration(f * float64(time.Second))
}
