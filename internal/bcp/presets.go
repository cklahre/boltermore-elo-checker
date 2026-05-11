package bcp

import "strings"

// GeoCenter is one pin for BCP’s geo event search (latitude, longitude).
type GeoCenter struct {
	Name string
	Lat  float64
	Lon  float64
}

// DefaultSearchTerms returns 3+ character tokens accepted by /v1/events search.
func DefaultSearchTerms() []string {
	raw := []string{
		"war", "hammer", "40k",
		"tournament", "championship", "grand", "open", "qualifier",
		"itc", "games", "battle", "hobby", "wargaming",
		"crusade", "narrative", "matched",
		"series", "classic", "cup", "fest", "con", "expo",
		"invitational", "masters", "nationals", "regional",
		"world", "throne", "league", "store",
		"2022", "2023", "2024", "2025", "2026",
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if len([]rune(t)) < 3 {
			continue
		}
		key := strings.ToLower(t)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, t)
	}
	return out
}

// PresetCenters returns map pins for the named coverage preset:
//   - global: one representative pin (BCP’s API + large -distance already returns
//     effectively catalog-wide 40k events; extra pins mostly duplicate HTTP work).
//   - global-grid: full Americas + Europe + Asia/Pacific grid (use for edge cases or with a small -distance).
//   - us: United States–heavy grid (+ Canada/Mexico)
//   - eu: Europe + UK
//   - apac: Asia + Oceania
//   - minimal: two pins (smoke test)
func PresetCenters(preset string) []GeoCenter {
	switch preset {
	case "minimal":
		return []GeoCenter{
			{Name: "US-mid-atlantic", Lat: 39.5, Lon: -77.0},
			{Name: "UK-london", Lat: 51.5, Lon: -0.12},
		}
	case "us":
		return usCenters()
	case "eu":
		return euCenters()
	case "apac":
		return apacCenters()
	case "global", "":
		return []GeoCenter{{Name: "catalog-wide-default", Lat: 40.71, Lon: -74.01}}
	case "global-grid":
		out := make([]GeoCenter, 0, len(usCenters())+len(euCenters())+len(apacCenters()))
		out = append(out, usCenters()...)
		out = append(out, euCenters()...)
		out = append(out, apacCenters()...)
		return out
	default:
		return nil
	}
}

func usCenters() []GeoCenter {
	return []GeoCenter{
		{Name: "NYC", Lat: 40.71, Lon: -74.01},
		{Name: "LA", Lat: 34.05, Lon: -118.24},
		{Name: "Chicago", Lat: 41.88, Lon: -87.63},
		{Name: "Houston", Lat: 29.76, Lon: -95.37},
		{Name: "Phoenix", Lat: 33.45, Lon: -112.07},
		{Name: "Philly", Lat: 39.95, Lon: -75.17},
		{Name: "Dallas", Lat: 32.78, Lon: -96.80},
		{Name: "Seattle", Lat: 47.61, Lon: -122.33},
		{Name: "Denver", Lat: 39.74, Lon: -104.99},
		{Name: "Atlanta", Lat: 33.75, Lon: -84.39},
		{Name: "Miami", Lat: 25.76, Lon: -80.19},
		{Name: "Minneapolis", Lat: 44.98, Lon: -93.27},
		{Name: "Detroit", Lat: 42.33, Lon: -83.05},
		{Name: "Tampa", Lat: 27.95, Lon: -82.46},
		{Name: "Orlando", Lat: 28.54, Lon: -81.38},
		{Name: "KC", Lat: 39.10, Lon: -94.58},
		{Name: "SLC", Lat: 40.76, Lon: -111.89},
		{Name: "Portland-OR", Lat: 45.52, Lon: -122.68},
		{Name: "Las-Vegas", Lat: 36.17, Lon: -115.14},
		{Name: "Nashville", Lat: 36.16, Lon: -86.78},
		{Name: "St-Louis", Lat: 38.63, Lon: -90.20},
		{Name: "Charlotte", Lat: 35.23, Lon: -80.84},
		{Name: "Raleigh", Lat: 35.78, Lon: -78.64},
		{Name: "Indianapolis", Lat: 39.77, Lon: -86.16},
		{Name: "Columbus-OH", Lat: 39.96, Lon: -83.00},
		{Name: "Madison", Lat: 43.07, Lon: -89.40},
		{Name: "Milwaukee", Lat: 43.04, Lon: -87.91},
		{Name: "Anchorage", Lat: 61.22, Lon: -149.90},
		{Name: "Honolulu", Lat: 21.31, Lon: -157.86},
		{Name: "Montreal", Lat: 45.50, Lon: -73.57},
		{Name: "Toronto", Lat: 43.65, Lon: -79.38},
		{Name: "Vancouver", Lat: 49.28, Lon: -123.12},
		{Name: "Calgary", Lat: 51.04, Lon: -114.07},
		{Name: "Mexico-City", Lat: 19.43, Lon: -99.13},
	}
}

func euCenters() []GeoCenter {
	return []GeoCenter{
		{Name: "London", Lat: 51.51, Lon: -0.13},
		{Name: "Manchester", Lat: 53.48, Lon: -2.24},
		{Name: "Glasgow", Lat: 55.86, Lon: -4.25},
		{Name: "Dublin", Lat: 53.35, Lon: -6.26},
		{Name: "Paris", Lat: 48.86, Lon: 2.35},
		{Name: "Lille", Lat: 50.63, Lon: 3.06},
		{Name: "Brussels", Lat: 50.85, Lon: 4.35},
		{Name: "Amsterdam", Lat: 52.37, Lon: 4.90},
		{Name: "Frankfurt", Lat: 50.11, Lon: 8.68},
		{Name: "Berlin", Lat: 52.52, Lon: 13.41},
		{Name: "Munich", Lat: 48.14, Lon: 11.58},
		{Name: "Hamburg", Lat: 53.55, Lon: 9.99},
		{Name: "Copenhagen", Lat: 55.68, Lon: 12.57},
		{Name: "Stockholm", Lat: 59.33, Lon: 18.07},
		{Name: "Oslo", Lat: 59.91, Lon: 10.75},
		{Name: "Warsaw", Lat: 52.23, Lon: 21.01},
		{Name: "Prague", Lat: 50.08, Lon: 14.44},
		{Name: "Vienna", Lat: 48.21, Lon: 16.37},
		{Name: "Zurich", Lat: 47.38, Lon: 8.54},
		{Name: "Milan", Lat: 45.46, Lon: 9.19},
		{Name: "Rome", Lat: 41.90, Lon: 12.50},
		{Name: "Madrid", Lat: 40.42, Lon: -3.70},
		{Name: "Barcelona", Lat: 41.39, Lon: 2.17},
		{Name: "Lisbon", Lat: 38.72, Lon: -9.14},
		{Name: "Athens", Lat: 37.98, Lon: 23.73},
		{Name: "Reykjavik", Lat: 64.15, Lon: -21.94},
	}
}

func apacCenters() []GeoCenter {
	return []GeoCenter{
		{Name: "Tokyo", Lat: 35.68, Lon: 139.76},
		{Name: "Osaka", Lat: 34.69, Lon: 135.50},
		{Name: "Seoul", Lat: 37.57, Lon: 127.03},
		{Name: "Singapore", Lat: 1.35, Lon: 103.82},
		{Name: "Sydney", Lat: -33.87, Lon: 151.21},
		{Name: "Melbourne", Lat: -37.81, Lon: 144.96},
		{Name: "Brisbane", Lat: -27.47, Lon: 153.03},
		{Name: "Auckland", Lat: -36.85, Lon: 174.76},
		{Name: "Taipei", Lat: 25.03, Lon: 121.56},
		{Name: "Hong-Kong", Lat: 22.32, Lon: 114.17},
	}
}
