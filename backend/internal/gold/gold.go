// Package gold aggregates gold prices from several exchanges / markets and
// the National Bank of Kazakhstan, normalising them to KZT per gram and
// USD per troy ounce using the NBK official exchange rates.
//
// All sources are free and keyless. Some (Yahoo Finance, Sina) are unofficial
// public endpoints, so every source is fetched independently and a failure of
// one only marks that row unavailable.
package gold

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	gramsPerOz = 31.1034768
	cacheKey   = "ag:gold:v1"
	cacheTTL   = 5 * time.Minute
	userAgent  = "Mozilla/5.0 (compatible; AuthGuard/1.0)"
)

// Kazakhstan has used a single UTC+5 time zone since March 2024. A fixed zone
// avoids depending on tzdata in the distroless image.
var almaty = time.FixedZone("Asia/Almaty", 5*3600)

type Quote struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Venue      string   `json:"venue"`
	Country    string   `json:"country"`
	Available  bool     `json:"available"`
	Price      float64  `json:"price,omitempty"`
	Currency   string   `json:"currency"`
	Unit       string   `json:"unit"` // "oz" or "g"
	ChangePct  *float64 `json:"change_pct,omitempty"`
	KZTPerGram float64  `json:"kzt_per_gram,omitempty"`
	USDPerOz   float64  `json:"usd_per_oz,omitempty"`
	AsOf       string   `json:"as_of,omitempty"`
	SourceURL  string   `json:"source_url"`
}

type Report struct {
	UpdatedAt time.Time          `json:"updated_at"`
	FX        map[string]float64 `json:"fx"`      // KZT per 1 unit of currency
	FXDate    string             `json:"fx_date"` // NBK rate date
	Quotes    []Quote            `json:"quotes"`
}

type Client struct {
	http  *http.Client
	redis *redis.Client
	log   *slog.Logger
	mu    sync.Mutex // collapses concurrent cache misses into one upstream fetch
}

func New(rdb *redis.Client, log *slog.Logger) *Client {
	return &Client{http: &http.Client{Timeout: 8 * time.Second}, redis: rdb, log: log}
}

func (c *Client) Report(ctx context.Context) (*Report, error) {
	if r, ok := c.fromCache(ctx); ok {
		return r, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if r, ok := c.fromCache(ctx); ok {
		return r, nil
	}

	fx, fxDate, err := c.nbkRates(ctx)
	if err != nil {
		return nil, fmt.Errorf("nbk exchange rates: %w", err)
	}

	sources := []struct {
		meta  Quote
		fetch func(context.Context) (Quote, error)
	}{
		{Quote{ID: "nbk", Name: "National Bank of Kazakhstan", Venue: "Official price, 1 g", Country: "KZ", Currency: "KZT", Unit: "g", SourceURL: "https://nationalbank.kz/ru/gold/zoloto"}, c.nbkGold},
		{Quote{ID: "spot", Name: "Spot XAU/USD", Venue: "International OTC", Country: "INTL", Currency: "USD", Unit: "oz", SourceURL: "https://gold-api.com"}, c.spot},
		{Quote{ID: "comex", Name: "COMEX", Venue: "Gold futures, New York", Country: "US", Currency: "USD", Unit: "oz", SourceURL: "https://www.cmegroup.com/markets/metals/precious/gold.html"}, c.comex},
		{Quote{ID: "sge", Name: "Shanghai Gold Exchange", Venue: "Au99.99, Shanghai", Country: "CN", Currency: "CNY", Unit: "g", SourceURL: "https://en.sge.com.cn"}, c.sge},
		{Quote{ID: "moex", Name: "Moscow Exchange", Venue: "GLDRUB_TOM, Moscow", Country: "RU", Currency: "RUB", Unit: "g", SourceURL: "https://www.moex.com/ru/issue/GLDRUB_TOM/CETS"}, c.moex},
	}

	quotes := make([]Quote, len(sources))
	var wg sync.WaitGroup
	for i, s := range sources {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, err := s.fetch(ctx)
			if err != nil {
				c.log.Warn("gold source failed", "source", s.meta.ID, "error", err)
				quotes[i] = s.meta
				return
			}
			// Keep static metadata, take live fields from the fetch.
			m := s.meta
			m.Available, m.Price, m.ChangePct, m.AsOf = true, q.Price, q.ChangePct, q.AsOf
			normalise(&m, fx)
			quotes[i] = m
		}()
	}
	wg.Wait()

	r := &Report{UpdatedAt: time.Now().UTC(), FX: fx, FXDate: fxDate, Quotes: quotes}
	available := 0
	for _, q := range quotes {
		if q.Available {
			available++
		}
	}
	if available == 0 {
		return nil, errors.New("all gold sources unavailable")
	}
	if b, err := json.Marshal(r); err == nil {
		ttl := cacheTTL
		if available < len(quotes) {
			ttl = time.Minute // retry failed sources sooner
		}
		_ = c.redis.Set(ctx, cacheKey, b, ttl).Err()
	}
	return r, nil
}

func (c *Client) fromCache(ctx context.Context) (*Report, bool) {
	b, err := c.redis.Get(ctx, cacheKey).Bytes()
	if err != nil {
		return nil, false
	}
	var r Report
	if json.Unmarshal(b, &r) != nil {
		return nil, false
	}
	return &r, true
}

func normalise(q *Quote, fx map[string]float64) {
	rate := 1.0
	if q.Currency != "KZT" {
		r, ok := fx[q.Currency]
		if !ok {
			return
		}
		rate = r
	}
	perGram := q.Price
	if q.Unit == "oz" {
		perGram = q.Price / gramsPerOz
	}
	q.KZTPerGram = round2(perGram * rate)
	if usd := fx["USD"]; usd > 0 {
		q.USDPerOz = round2(perGram * rate / usd * gramsPerOz)
	}
}

// ---------------------------------------------------------------- sources

// nbkRates reads the National Bank of Kazakhstan official rates (KZT per unit).
func (c *Client) nbkRates(ctx context.Context) (map[string]float64, string, error) {
	date := time.Now().In(almaty).Format("02.01.2006")
	body, err := c.get(ctx, "https://nationalbank.kz/rss/get_rates.cfm?fdate="+date, nil)
	if err != nil {
		return nil, "", err
	}
	var doc struct {
		Date  string `xml:"date"`
		Items []struct {
			Title string `xml:"title"`
			Value string `xml:"description"`
			Quant string `xml:"quant"`
		} `xml:"item"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return nil, "", err
	}
	fx := map[string]float64{}
	for _, it := range doc.Items {
		switch it.Title {
		case "USD", "CNY", "RUB", "EUR":
			v, err1 := strconv.ParseFloat(strings.TrimSpace(it.Value), 64)
			n, err2 := strconv.ParseFloat(strings.TrimSpace(it.Quant), 64)
			if err1 == nil && err2 == nil && n > 0 {
				fx[it.Title] = v / n
			}
		}
	}
	if fx["USD"] == 0 {
		return nil, "", errors.New("USD rate missing")
	}
	if doc.Date == "" {
		doc.Date = date
	}
	return fx, doc.Date, nil
}

var (
	nbkRow  = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	nbkCell = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	tags    = regexp.MustCompile(`<[^>]+>`)
)

// nbkGold scrapes the NBK "price of 1 gram of gold in tenge" table.
func (c *Client) nbkGold(ctx context.Context) (Quote, error) {
	body, err := c.get(ctx, "https://nationalbank.kz/ru/gold/zoloto", nil)
	if err != nil {
		return Quote{}, err
	}
	type point struct {
		date  string
		value float64
	}
	var pts []point
	for _, row := range nbkRow.FindAllStringSubmatch(string(body), -1) {
		cells := nbkCell.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 4 {
			continue
		}
		text := func(i int) string {
			return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(cells[i][1], "")))
		}
		v, err := parseNumber(text(3))
		if err != nil {
			continue
		}
		pts = append(pts, point{text(2), v})
		if len(pts) == 2 {
			break
		}
	}
	if len(pts) == 0 {
		return Quote{}, errors.New("no rows in NBK gold table")
	}
	q := Quote{Price: pts[0].value, AsOf: pts[0].date}
	if len(pts) == 2 && pts[1].value > 0 {
		q.ChangePct = pct(pts[0].value, pts[1].value)
	}
	return q, nil
}

func (c *Client) spot(ctx context.Context) (Quote, error) {
	var r struct {
		Price     float64 `json:"price"`
		UpdatedAt string  `json:"updatedAt"`
	}
	if err := c.getJSON(ctx, "https://api.gold-api.com/price/XAU", nil, &r); err != nil {
		return Quote{}, err
	}
	if r.Price <= 0 {
		return Quote{}, errors.New("no price")
	}
	return Quote{Price: r.Price, AsOf: r.UpdatedAt}, nil
}

func (c *Client) comex(ctx context.Context) (Quote, error) {
	var r struct {
		Chart struct {
			Result []struct {
				Meta struct {
					Price     float64  `json:"regularMarketPrice"`
					PrevClose float64  `json:"chartPreviousClose"`
					ChangePct *float64 `json:"regularMarketChangePercent"`
					Time      int64    `json:"regularMarketTime"`
				} `json:"meta"`
			} `json:"result"`
		} `json:"chart"`
	}
	if err := c.getJSON(ctx, "https://query1.finance.yahoo.com/v8/finance/chart/GC=F?range=5d&interval=1d", nil, &r); err != nil {
		return Quote{}, err
	}
	if len(r.Chart.Result) == 0 || r.Chart.Result[0].Meta.Price <= 0 {
		return Quote{}, errors.New("no price")
	}
	m := r.Chart.Result[0].Meta
	q := Quote{Price: m.Price, AsOf: time.Unix(m.Time, 0).UTC().Format(time.RFC3339)}
	switch {
	case m.ChangePct != nil:
		v := round2(*m.ChangePct)
		q.ChangePct = &v
	case m.PrevClose > 0:
		q.ChangePct = pct(m.Price, m.PrevClose)
	}
	return q, nil
}

// sge reads Shanghai Gold Exchange Au99.99 (CNY/g) from Sina Finance's public quote feed.
func (c *Client) sge(ctx context.Context) (Quote, error) {
	body, err := c.get(ctx, "https://hq.sinajs.cn/list=SGE_AU9999", map[string]string{"Referer": "https://finance.sina.com.cn"})
	if err != nil {
		return Quote{}, err
	}
	s := string(body)
	start, end := strings.IndexByte(s, '"'), strings.LastIndexByte(s, '"')
	if start < 0 || end <= start {
		return Quote{}, errors.New("unexpected format")
	}
	f := strings.Split(s[start+1:end], ",")
	if len(f) < 18 {
		return Quote{}, errors.New("unexpected field count")
	}
	price, err := strconv.ParseFloat(f[3], 64)
	if err != nil || price <= 0 {
		return Quote{}, errors.New("no price")
	}
	q := Quote{Price: price, AsOf: f[len(f)-2]}
	if v, err := strconv.ParseFloat(strings.TrimSuffix(f[len(f)-1], "%"), 64); err == nil {
		q.ChangePct = &v
	}
	return q, nil
}

func (c *Client) moex(ctx context.Context) (Quote, error) {
	var r struct {
		MarketData struct {
			Data [][]any `json:"data"`
		} `json:"marketdata"`
	}
	u := "https://iss.moex.com/iss/engines/currency/markets/selt/boards/CETS/securities/GLDRUB_TOM.json" +
		"?iss.meta=off&iss.only=marketdata&marketdata.columns=LAST,LASTTOPREVPRICE,SYSTIME"
	if err := c.getJSON(ctx, u, nil, &r); err != nil {
		return Quote{}, err
	}
	if len(r.MarketData.Data) == 0 || len(r.MarketData.Data[0]) < 3 {
		return Quote{}, errors.New("no data")
	}
	row := r.MarketData.Data[0]
	price, ok := row[0].(float64)
	if !ok || price <= 0 {
		return Quote{}, errors.New("no trades")
	}
	q := Quote{Price: price}
	if v, ok := row[1].(float64); ok {
		v = round2(v)
		q.ChangePct = &v
	}
	if t, ok := row[2].(string); ok {
		q.AsOf = t
	}
	return q, nil
}

// ---------------------------------------------------------------- http helpers

func (c *Client) get(ctx context.Context, url string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 2<<20))
}

func (c *Client) getJSON(ctx context.Context, url string, headers map[string]string, dst any) error {
	b, err := c.get(ctx, url, headers)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func parseNumber(s string) (float64, error) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', ' ', ' ':
			return -1
		case ',':
			return '.'
		}
		return r
	}, s)
	return strconv.ParseFloat(s, 64)
}

func pct(cur, prev float64) *float64 {
	v := round2((cur - prev) / prev * 100)
	return &v
}

func round2(v float64) float64 { return float64(int64(v*100+sign(v)*0.5)) / 100 }

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// ---------------------------------------------------------------- history

const (
	historyKey     = "ag:gold:hist:v1"
	historyTTL     = 3 * time.Hour
	historyDays    = 366
	historyMaxPage = 12 // NBK lists 30 business days per page; 12 pages ≈ 16 months
)

type Point struct {
	Date  string  `json:"date"` // YYYY-MM-DD
	Value float64 `json:"value"`
}

type History struct {
	Series    string    `json:"series"`
	Unit      string    `json:"unit"`
	Points    []Point   `json:"points"` // ascending by date
	UpdatedAt time.Time `json:"updated_at"`
}

// History returns about one year of the NBK official price of 1 gram of gold in tenge.
func (c *Client) History(ctx context.Context) (*History, error) {
	if b, err := c.redis.Get(ctx, historyKey).Bytes(); err == nil {
		var h History
		if json.Unmarshal(b, &h) == nil {
			return &h, nil
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	cutoff := time.Now().In(almaty).AddDate(0, 0, -historyDays).Format("2006-01-02")
	pages := make([][]Point, historyMaxPage)
	errs := make([]error, historyMaxPage)
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4) // be polite to nationalbank.kz
	for i := range historyMaxPage {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pages[i], errs[i] = c.nbkPage(ctx, i+1)
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	var pts []Point
	for i, p := range pages {
		if errs[i] != nil {
			if i == 0 {
				return nil, errs[i]
			}
			c.log.Warn("nbk history page failed", "page", i+1, "error", errs[i])
			continue
		}
		for _, pt := range p {
			if pt.Date >= cutoff && !seen[pt.Date] {
				seen[pt.Date] = true
				pts = append(pts, pt)
			}
		}
	}
	if len(pts) == 0 {
		return nil, errors.New("no history points")
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].Date < pts[j].Date })

	h := &History{Series: "NBK official gold price", Unit: "KZT/g", Points: pts, UpdatedAt: time.Now().UTC()}
	if b, err := json.Marshal(h); err == nil {
		_ = c.redis.Set(ctx, historyKey, b, historyTTL).Err()
	}
	return h, nil
}

var isoDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (c *Client) nbkPage(ctx context.Context, page int) ([]Point, error) {
	body, err := c.get(ctx, "https://nationalbank.kz/ru/gold/zoloto?page="+strconv.Itoa(page), nil)
	if err != nil {
		return nil, err
	}
	var pts []Point
	for _, row := range nbkRow.FindAllStringSubmatch(string(body), -1) {
		cells := nbkCell.FindAllStringSubmatch(row[1], -1)
		if len(cells) < 4 {
			continue
		}
		text := func(i int) string {
			return strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(cells[i][1], "")))
		}
		d := text(2)
		v, err := parseNumber(text(3))
		if err != nil || !isoDate.MatchString(d) || v <= 0 {
			continue
		}
		pts = append(pts, Point{Date: d, Value: v})
	}
	return pts, nil
}
