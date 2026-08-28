package briefing

import (
	"encoding/json"
	"math"
)

// LatLng in degrees, WGS84.
type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// DecodePolyline decodes a Google encoded polyline (precision 1e-5).
func DecodePolyline(s string) []LatLng {
	var out []LatLng
	var lat, lng int64
	i := 0
	for i < len(s) {
		var result, shift int64
		for {
			b := int64(s[i]) - 63
			i++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 || i >= len(s) {
				break
			}
		}
		dlat := result >> 1
		if result&1 != 0 {
			dlat = ^dlat
		}
		lat += dlat
		if i >= len(s) {
			break
		}
		result, shift = 0, 0
		for {
			b := int64(s[i]) - 63
			i++
			result |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 || i >= len(s) {
				break
			}
		}
		dlng := result >> 1
		if result&1 != 0 {
			dlng = ^dlng
		}
		lng += dlng
		out = append(out, LatLng{Lat: float64(lat) / 1e5, Lng: float64(lng) / 1e5})
	}
	return out
}

// Equirectangular projection around a reference latitude: metres, good to
// <1% at city scale (São Paulo spans ~0.5°).
const (
	metresPerDegLat = 110_540.0
	metresPerDegLng = 111_320.0
)

func project(p LatLng, cosLat float64) (x, y float64) {
	return p.Lng * metresPerDegLng * cosLat, p.Lat * metresPerDegLat
}

// DistanceToPathMetres returns the shortest distance from p to the polyline.
func DistanceToPathMetres(p LatLng, path []LatLng) float64 {
	if len(path) == 0 {
		return math.Inf(1)
	}
	cosLat := math.Cos(p.Lat * math.Pi / 180)
	px, py := project(p, cosLat)
	best := math.Inf(1)
	if len(path) == 1 {
		x, y := project(path[0], cosLat)
		return math.Hypot(px-x, py-y)
	}
	for i := 0; i+1 < len(path); i++ {
		ax, ay := project(path[i], cosLat)
		bx, by := project(path[i+1], cosLat)
		d := pointSegment(px, py, ax, ay, bx, by)
		if d < best {
			best = d
		}
	}
	return best
}

func pointSegment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / l2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// Polygon rings in GeoJSON order: [ring][vertex][lng, lat].
type polygon [][][2]float64

// PointInPolygon: ray casting over the outer ring; holes are rare in alert
// polygons and ignored (conservative — treats a hole as inside).
func (pg polygon) contains(p LatLng) bool {
	if len(pg) == 0 {
		return false
	}
	ring := pg[0]
	inside := false
	for i, j := 0, len(ring)-1; i < len(ring); j, i = i, i+1 {
		xi, yi := ring[i][0], ring[i][1]
		xj, yj := ring[j][0], ring[j][1]
		if (yi > p.Lat) != (yj > p.Lat) && p.Lng < (xj-xi)*(p.Lat-yi)/(yj-yi)+xi {
			inside = !inside
		}
	}
	return inside
}

// parseMultiPolygon accepts GeoJSON Polygon or MultiPolygon (the Weather API
// ships alert polygons as a JSON *string* of a MultiPolygon).
func parseMultiPolygon(raw json.RawMessage) ([]polygon, error) {
	var g struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	switch g.Type {
	case "Polygon":
		var pg polygon
		if err := json.Unmarshal(g.Coordinates, &pg); err != nil {
			return nil, err
		}
		return []polygon{pg}, nil
	case "MultiPolygon":
		var mp []polygon
		if err := json.Unmarshal(g.Coordinates, &mp); err != nil {
			return nil, err
		}
		return mp, nil
	}
	return nil, nil
}

func pathCrosses(path []LatLng, polys []polygon) bool {
	for _, p := range path {
		for _, pg := range polys {
			if pg.contains(p) {
				return true
			}
		}
	}
	return false
}
