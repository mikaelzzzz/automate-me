package briefing

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// GeoSampa "risco de ocorrência de alagamento": flooding occurrences logged by
// São Paulo's Civil Defense (SIGRC), 2013→. August is dry season, so live
// flood alerts are usually empty; this historic layer always has data and
// tells the user "your route crosses N points with flooding history".
//
//go:embed data/geosampa_alagamento.geojson
var geosampaRaw []byte

// FloodPoint is one historic flooding occurrence.
type FloodPoint struct {
	LatLng
	Date          string `json:"date"`
	Subprefecture string `json:"subprefecture"`
}

var (
	floodOnce   sync.Once
	floodPoints []FloodPoint
)

// HistoricFloodPoints returns the embedded layer (parsed once).
func HistoricFloodPoints() []FloodPoint {
	floodOnce.Do(func() {
		var fc struct {
			Features []struct {
				Geometry struct {
					Coordinates [2]float64 `json:"coordinates"`
				} `json:"geometry"`
				Properties struct {
					Date          string `json:"date"`
					Kind          string `json:"kind"`
					Subprefecture string `json:"subprefecture"`
				} `json:"properties"`
			} `json:"features"`
		}
		if err := json.Unmarshal(geosampaRaw, &fc); err != nil {
			return
		}
		for _, f := range fc.Features {
			floodPoints = append(floodPoints, FloodPoint{
				LatLng:        LatLng{Lat: f.Geometry.Coordinates[1], Lng: f.Geometry.Coordinates[0]},
				Date:          strings.TrimSuffix(f.Properties.Date, "Z"),
				Subprefecture: cleanSubprefecture(f.Properties.Subprefecture),
			})
		}
	})
	return floodPoints
}

// "VP - VILA PRUDENTE" → "Vila Prudente"; "AF - ARICANDUVA/VILA FORMOSA" →
// "Aricanduva/Vila Formosa".
func cleanSubprefecture(s string) string {
	if i := strings.Index(s, " - "); i >= 0 {
		s = s[i+3:]
	}
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		if len(w) <= 2 && i > 0 {
			continue // "de", "da"
		}
		parts := strings.Split(w, "/")
		for j, pt := range parts {
			if pt != "" {
				parts[j] = strings.ToUpper(pt[:1]) + pt[1:]
			}
		}
		words[i] = strings.Join(parts, "/")
	}
	return strings.Join(words, " ")
}

// FloodPointsNear returns the historic points within radius metres of the
// route, nearest first.
func FloodPointsNear(path []LatLng, radiusM float64) []FloodPoint {
	type hit struct {
		p FloodPoint
		d float64
	}
	var hits []hit
	for _, fp := range HistoricFloodPoints() {
		if d := DistanceToPathMetres(fp.LatLng, path); d <= radiusM {
			hits = append(hits, hit{fp, d})
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].d < hits[j].d })
	out := make([]FloodPoint, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.p)
	}
	return out
}

// Subprefectures lists distinct neighbourhoods of the given points, in order.
func Subprefectures(points []FloodPoint) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range points {
		if p.Subprefecture == "" || seen[p.Subprefecture] {
			continue
		}
		seen[p.Subprefecture] = true
		out = append(out, p.Subprefecture)
	}
	return out
}
