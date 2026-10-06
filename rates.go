package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const fredObservationsURL = "https://api.stlouisfed.org/fred/series/observations"
const rateConvention = "Treasury par-as-zero proxy: <=6m simple ACT/365; >6m semiannual; linear continuous-rate interpolation; flat endpoint"
const maxRateAgeDays = 7

type rateTenor struct {
	Series string
	Years  float64
}

var treasuryTenors = []rateTenor{{"DGS1MO", 1.0 / 12}, {"DGS3MO", .25}, {"DGS6MO", .5}, {"DGS1", 1}, {"DGS2", 2}, {"DGS3", 3}, {"DGS5", 5}, {"DGS7", 7}, {"DGS10", 10}, {"DGS20", 20}, {"DGS30", 30}}

type RatePoint struct {
	Series       string
	Years        float64
	YieldPercent float64
	Date         string
}
type RateSelection struct {
	Rate                            float64
	Source, ObservationDate, Method string
}

func requiredTenors(days []float64) ([]rateTenor, error) {
	needed := map[string]bool{}
	for _, d := range days {
		if !isFinite(d) || d < 0 || d/365 > 30 {
			return nil, fmt.Errorf("FRED maturity must be between 0 and 30 years; use manual input outside this range")
		}
		t := d / 365
		index := sort.Search(len(treasuryTenors), func(i int) bool { return treasuryTenors[i].Years >= t })
		if index == len(treasuryTenors) {
			index--
		}
		needed[treasuryTenors[index].Series] = true
		if index > 0 && treasuryTenors[index].Years != t {
			needed[treasuryTenors[index-1].Series] = true
		}
	}
	var selected []rateTenor
	for _, tenor := range treasuryTenors {
		if needed[tenor.Series] {
			selected = append(selected, tenor)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("empty expiration grid")
	}
	return selected, nil
}

func continuousProxy(p RatePoint) (float64, error) {
	y := p.YieldPercent / 100
	if !isFinite(y) || y <= -1 || y > 1 || !isFinitePositive(p.Years) {
		return 0, fmt.Errorf("invalid Treasury yield")
	}
	// This intentionally documented approximation does NOT bootstrap a zero curve.
	if p.Years <= .5 {
		return math.Log1p(y*p.Years) / p.Years, nil
	}
	return 2 * math.Log1p(y/2), nil
}

func selectRate(points []RatePoint, days float64, source string) (RateSelection, error) {
	if len(points) == 0 || !isFinite(days) || days < 0 {
		return RateSelection{}, fmt.Errorf("invalid rate curve or expiration")
	}
	t := days / 365
	i := sort.Search(len(points), func(i int) bool { return points[i].Years >= t })
	lo, hi := i, i
	if i == len(points) {
		lo, hi = len(points)-1, len(points)-1
	} else if i > 0 && points[i].Years != t {
		lo = i - 1
	}
	a, b := points[lo], points[hi]
	ra, err := continuousProxy(a)
	if err != nil {
		return RateSelection{}, err
	}
	rb, err := continuousProxy(b)
	if err != nil {
		return RateSelection{}, err
	}
	rate := ra
	series := a.Series
	if lo != hi {
		rate = ra + (rb-ra)*(t-a.Years)/(b.Years-a.Years)
		series += "+" + b.Series
	}
	return RateSelection{Rate: rate, Source: source + " " + series, ObservationDate: a.Date, Method: rateConvention}, nil
}

func validateRatePoints(points []RatePoint, tenors []rateTenor, now time.Time) error {
	if len(points) != len(tenors) {
		return fmt.Errorf("incomplete Treasury curve")
	}
	today, err := time.Parse("2006-01-02", now.Format("2006-01-02"))
	if err != nil {
		return err
	}
	for i, p := range points {
		if p.Series != tenors[i].Series || p.Years != tenors[i].Years {
			return fmt.Errorf("unexpected Treasury tenor")
		}
		if _, err := continuousProxy(p); err != nil {
			return err
		}
		date, err := time.Parse("2006-01-02", p.Date)
		if err != nil || date.After(today) || today.Sub(date) > maxRateAgeDays*24*time.Hour {
			return fmt.Errorf("Treasury observation is invalid, future-dated, or older than %d days", maxRateAgeDays)
		}
		if p.Date != points[0].Date {
			return fmt.Errorf("Treasury observations must share a date")
		}
	}
	return nil
}

func fetchTreasuryCurve(ctx context.Context, client *http.Client, endpoint, key string, tenors []rateTenor, now time.Time) ([]RatePoint, error) {
	if strings.TrimSpace(key) == "" {
		return nil, fmt.Errorf("FRED_API_KEY is not set")
	}
	if client == nil {
		return nil, fmt.Errorf("FRED HTTP client is nil")
	}
	observations := make([]map[string]float64, len(tenors))
	for i, tenor := range tenors {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("invalid FRED endpoint")
		}
		q := u.Query()
		q.Set("api_key", key)
		q.Set("series_id", tenor.Series)
		q.Set("file_type", "json")
		q.Set("sort_order", "desc")
		q.Set("observation_start", now.AddDate(0, 0, -14).Format("2006-01-02"))
		q.Set("observation_end", now.Format("2006-01-02"))
		u.RawQuery = q.Encode()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, fmt.Errorf("cannot create FRED request")
		}
		response, err := client.Do(request)
		// net/url errors can include the query-string API key; never echo them.
		if err != nil {
			return nil, fmt.Errorf("FRED request failed for %s (connection, timeout, or cancellation)", tenor.Series)
		}
		var payload struct {
			Observations []struct {
				Date  string `json:"date"`
				Value string `json:"value"`
			} `json:"observations"`
		}
		if response.StatusCode != http.StatusOK {
			response.Body.Close()
			return nil, fmt.Errorf("FRED %s returned HTTP %d", tenor.Series, response.StatusCode)
		}
		err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload)
		response.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("invalid FRED response for %s", tenor.Series)
		}
		observations[i] = map[string]float64{}
		for _, o := range payload.Observations {
			if o.Value == "." {
				continue
			}
			v, err := strconv.ParseFloat(o.Value, 64)
			if err != nil || !isFinite(v) {
				continue
			}
			observations[i][o.Date] = v
		}
	}
	if len(observations) == 0 {
		return nil, fmt.Errorf("no Treasury tenors requested")
	}
	// Pick the latest common valid date, not a mixture of dates across maturities.
	var dates []string
	for date := range observations[0] {
		dates = append(dates, date)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	for _, date := range dates {
		var points []RatePoint
		for i, tenor := range tenors {
			v, ok := observations[i][date]
			if !ok {
				break
			}
			points = append(points, RatePoint{Series: tenor.Series, Years: tenor.Years, YieldPercent: v, Date: date})
		}
		if validateRatePoints(points, tenors, now) == nil {
			return points, nil
		}
	}
	return nil, fmt.Errorf("FRED has no common valid observation within %d days", maxRateAgeDays)
}

type rateLoader func(context.Context, []float64) ([]RatePoint, string, error)

func loadTreasuryRates(ctx context.Context, days []float64) ([]RatePoint, string, error) {
	return loadTreasuryRatesWith(ctx, days, &http.Client{Timeout: 10 * time.Second}, fredObservationsURL, os.Getenv("FRED_API_KEY"), filepath.Join(".cache", "fred-rates.json"), time.Now().UTC())
}

func loadTreasuryRatesWith(ctx context.Context, days []float64, client *http.Client, endpoint, key, cachePath string, now time.Time) ([]RatePoint, string, error) {
	tenors, err := requiredTenors(days)
	if err != nil {
		return nil, "", err
	}
	points, fetchErr := fetchTreasuryCurve(ctx, client, endpoint, key, tenors, now)
	if fetchErr == nil {
		// Cache contains public observations only, never credentials. Failed cache writes
		// are surfaced but do not prevent use of a valid downloaded observation.
		data, _ := json.MarshalIndent(points, "", "  ")
		err = os.MkdirAll(filepath.Dir(cachePath), 0700)
		if err == nil {
			var file *os.File
			file, err = os.CreateTemp(filepath.Dir(cachePath), "fred-*.json")
			if err == nil {
				temp := file.Name()
				_, err = file.Write(data)
				closeErr := file.Close()
				if err == nil {
					err = closeErr
				}
				if err == nil {
					err = os.Rename(temp, cachePath)
				}
				os.Remove(temp)
			}
		}
		if err != nil {
			return points, "FRED (cache write unavailable)", nil
		}
		return points, "FRED", nil
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, "", fetchErr
	}
	var cached []RatePoint
	if json.Unmarshal(data, &cached) != nil {
		return nil, "", fetchErr
	}
	var subset []RatePoint
	for _, tenor := range tenors {
		for _, p := range cached {
			if p.Series == tenor.Series {
				subset = append(subset, p)
				break
			}
		}
	}
	if validateRatePoints(subset, tenors, now) != nil {
		return nil, "", fmt.Errorf("%v; no complete fresh cache available", fetchErr)
	}
	return subset, "cached FRED (live fetch unavailable)", nil
}

func configureRates(ctx context.Context, reader *bufio.Reader, writer io.Writer, sim *MonteCarlo, loader rateLoader) error {
	fmt.Fprint(writer, "Rate source: FRED or manual [FRED]: ")
	mode, err := readLine(reader)
	if err != nil {
		return err
	}
	for mode != "" && !strings.EqualFold(mode, "FRED") && !strings.EqualFold(mode, "F") && !strings.EqualFold(mode, "MANUAL") && !strings.EqualFold(mode, "M") {
		fmt.Fprint(writer, "Enter FRED or manual: ")
		mode, err = readLine(reader)
		if err != nil {
			return err
		}
	}
	if mode == "" || strings.EqualFold(mode, "FRED") || strings.EqualFold(mode, "F") {
		points, source, loadErr := loader(ctx, sim.ExpirationDays)
		if loadErr == nil {
			selections := make([]RateSelection, len(sim.ExpirationDays))
			for i, days := range sim.ExpirationDays {
				selections[i], err = selectRate(points, days, source)
				if err != nil {
					return err
				}
				fmt.Fprintf(writer, "%.0f DTE: %.5f%% continuous, %s, observed %s\n", days, selections[i].Rate*100, selections[i].Source, selections[i].ObservationDate)
			}
			fmt.Fprintln(writer, "Treasury par-as-zero approximation; constant rate per contract, not a bootstrapped discount curve.")
			choice, err := promptChoice(reader, writer, "Use these rates or enter a manual override (Use/Manual): ", map[string]string{"USE": "USE", "U": "USE", "MANUAL": "MANUAL", "M": "MANUAL"})
			if err != nil {
				return err
			}
			if choice == "USE" {
				sim.Rates = selections
				sim.RiskFreeRate = selections[0].Rate
				return nil
			}
		} else {
			fmt.Fprintf(writer, "Automatic rates unavailable: %v. Enter a manual rate.\n", loadErr)
		}
	}
	rate, err := promptFloat(reader, writer, "Annual continuously compounded rate (%): ", nil, func(v float64) bool { return isFinite(v) && v > -100 && v <= 100 })
	if err != nil {
		return err
	}
	sim.RiskFreeRate = rate / 100
	sim.Rates = make([]RateSelection, len(sim.ExpirationDays))
	for i := range sim.Rates {
		sim.Rates[i] = RateSelection{Rate: sim.RiskFreeRate, Source: "manual", Method: "user-supplied continuously compounded annual rate"}
	}
	return nil
}

func (sim *MonteCarlo) rateAt(column int) RateSelection {
	if column < len(sim.Rates) {
		return sim.Rates[column]
	}
	return RateSelection{Rate: sim.RiskFreeRate, Source: "manual", Method: "user-supplied continuously compounded annual rate"}
}
