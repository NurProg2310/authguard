// Package weather fetches forecasts and city search results from Open-Meteo
// (https://open-meteo.com, no API key required) and caches them in Redis.
package weather

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	forecastURL  = "https://api.open-meteo.com/v1/forecast"
	geocodingURL = "https://geocoding-api.open-meteo.com/v1/search"
	forecastTTL  = 10 * time.Minute
	searchTTL    = 6 * time.Hour
)

var ErrUpstream = errors.New("weather provider unavailable")

type Current struct {
	Time         string  `json:"time"`
	Temperature  float64 `json:"temperature"`
	ApparentTemp float64 `json:"apparent_temperature"`
	Humidity     float64 `json:"humidity"`
	WindSpeed    float64 `json:"wind_speed"`
	WeatherCode  int     `json:"weather_code"`
	IsDay        bool    `json:"is_day"`
}

type Day struct {
	Date        string  `json:"date"`
	WeatherCode int     `json:"weather_code"`
	TempMax     float64 `json:"temp_max"`
	TempMin     float64 `json:"temp_min"`
}

type Forecast struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  string  `json:"timezone"`
	Current   Current `json:"current"`
	Daily     []Day   `json:"daily"`
}

type Place struct {
	Name      string  `json:"name"`
	Region    string  `json:"region,omitempty"`
	Country   string  `json:"country,omitempty"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type Client struct {
	http  *http.Client
	redis *redis.Client
}

func New(rdb *redis.Client) *Client {
	return &Client{http: &http.Client{Timeout: 6 * time.Second}, redis: rdb}
}

// Forecast returns current conditions plus a 5-day outlook. Coordinates are
// rounded to ~1 km so nearby users share a cache entry.
func (c *Client) Forecast(ctx context.Context, lat, lon float64) (*Forecast, error) {
	la, lo := strconv.FormatFloat(lat, 'f', 2, 64), strconv.FormatFloat(lon, 'f', 2, 64)
	var out Forecast
	err := c.cached(ctx, "ag:wx:f:"+la+":"+lo, forecastTTL, &out, func() (any, error) {
		q := url.Values{
			"latitude":      {la},
			"longitude":     {lo},
			"current":       {"temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m,is_day"},
			"daily":         {"weather_code,temperature_2m_max,temperature_2m_min"},
			"timezone":      {"auto"},
			"forecast_days": {"5"},
		}
		var raw struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Timezone  string  `json:"timezone"`
			Current   struct {
				Time        string  `json:"time"`
				Temperature float64 `json:"temperature_2m"`
				Apparent    float64 `json:"apparent_temperature"`
				Humidity    float64 `json:"relative_humidity_2m"`
				Code        int     `json:"weather_code"`
				Wind        float64 `json:"wind_speed_10m"`
				IsDay       int     `json:"is_day"`
			} `json:"current"`
			Daily struct {
				Time []string  `json:"time"`
				Code []int     `json:"weather_code"`
				Max  []float64 `json:"temperature_2m_max"`
				Min  []float64 `json:"temperature_2m_min"`
			} `json:"daily"`
		}
		if err := c.getJSON(ctx, forecastURL+"?"+q.Encode(), &raw); err != nil {
			return nil, err
		}
		f := Forecast{
			Latitude: raw.Latitude, Longitude: raw.Longitude, Timezone: raw.Timezone,
			Current: Current{
				Time: raw.Current.Time, Temperature: raw.Current.Temperature, ApparentTemp: raw.Current.Apparent,
				Humidity: raw.Current.Humidity, WindSpeed: raw.Current.Wind, WeatherCode: raw.Current.Code,
				IsDay: raw.Current.IsDay == 1,
			},
		}
		d := raw.Daily
		for i := range d.Time {
			if i >= len(d.Code) || i >= len(d.Max) || i >= len(d.Min) {
				break
			}
			f.Daily = append(f.Daily, Day{Date: d.Time[i], WeatherCode: d.Code[i], TempMax: d.Max[i], TempMin: d.Min[i]})
		}
		return f, nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// Search looks up cities by name.
func (c *Client) Search(ctx context.Context, name, lang string) ([]Place, error) {
	out := []Place{}
	err := c.cached(ctx, "ag:wx:s:"+lang+":"+name, searchTTL, &out, func() (any, error) {
		q := url.Values{"name": {name}, "count": {"6"}, "language": {lang}, "format": {"json"}}
		var raw struct {
			Results []struct {
				Name      string  `json:"name"`
				Admin1    string  `json:"admin1"`
				Country   string  `json:"country"`
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
			} `json:"results"`
		}
		if err := c.getJSON(ctx, geocodingURL+"?"+q.Encode(), &raw); err != nil {
			return nil, err
		}
		places := make([]Place, 0, len(raw.Results))
		for _, r := range raw.Results {
			places = append(places, Place{Name: r.Name, Region: r.Admin1, Country: r.Country, Latitude: r.Latitude, Longitude: r.Longitude})
		}
		return places, nil
	})
	return out, err
}

// cached serves dst from Redis when present, otherwise calls fetch and stores
// the result. Redis errors degrade to an uncached upstream call.
func (c *Client) cached(ctx context.Context, key string, ttl time.Duration, dst any, fetch func() (any, error)) error {
	if b, err := c.redis.Get(ctx, key).Bytes(); err == nil && json.Unmarshal(b, dst) == nil {
		return nil
	}
	v, err := fetch()
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_ = c.redis.Set(ctx, key, b, ttl).Err()
	return json.Unmarshal(b, dst)
}

func (c *Client) getJSON(ctx context.Context, u string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "AuthGuard/1.0")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrUpstream, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst); err != nil {
		return fmt.Errorf("%w: decode: %v", ErrUpstream, err)
	}
	return nil
}
