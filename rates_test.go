package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rateTestNow() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }

func TestTreasuryInterpolationAndConventions(t *testing.T) {
	pts := []RatePoint{{"DGS1MO", 1.0 / 12, 4, "2026-10-02"}, {"DGS3MO", .25, 5, "2026-10-02"}}
	lo, _ := continuousProxy(pts[0])
	hi, _ := continuousProxy(pts[1])
	if math.Abs(lo-math.Log1p(.04/12)*12) > 1e-14 {
		t.Fatal("short-tenor conversion")
	}
	long, _ := continuousProxy(RatePoint{Years: 1, YieldPercent: 4})
	if math.Abs(long-2*math.Log1p(.04/2)) > 1e-14 {
		t.Fatal("semiannual conversion")
	}
	r, err := selectRate(pts, 365.0/6, "FRED")
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(r.Rate-(lo+hi)/2) > 1e-14 || r.ObservationDate != "2026-10-02" || !strings.Contains(r.Source, "DGS1MO+DGS3MO") {
		t.Fatalf("interpolation %+v", r)
	}
	r, err = selectRate(pts, 7, "FRED")
	if err != nil || r.Rate != lo {
		t.Fatal("short end should be explicit flat proxy")
	}
	tenors, err := requiredTenors([]float64{7, 35, 100, 365})
	if err != nil {
		t.Fatal(err)
	}
	if len(tenors) != 4 {
		t.Fatalf("wrong brackets: %+v", tenors)
	}
	if _, err := requiredTenors([]float64{31 * 365}); err == nil {
		t.Fatal("accepted maturity outside supported curve")
	}
}

func TestFREDLatestCommonDateMissingAndFuture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "dummy" || r.URL.Query().Get("file_type") != "json" || r.URL.Query().Get("observation_end") != "2026-10-05" {
			t.Error("incorrect request")
		}
		if r.URL.Query().Get("series_id") == "DGS1MO" {
			fmt.Fprint(w, `{"observations":[{"date":"2026-10-06","value":"4"},{"date":"2026-10-05","value":"."},{"date":"2026-10-02","value":"4.1"},{"date":"2026-10-01","value":"4"}]}`)
		} else {
			fmt.Fprint(w, `{"observations":[{"date":"2026-10-06","value":"4"},{"date":"2026-10-02","value":"."},{"date":"2026-10-01","value":"4.5"}]}`)
		}
	}))
	defer server.Close()
	tenors, _ := requiredTenors([]float64{45})
	pts, err := fetchTreasuryCurve(context.Background(), server.Client(), server.URL, "dummy", tenors, rateTestNow())
	if err != nil {
		t.Fatal(err)
	}
	if len(pts) != 2 || pts[0].Date != "2026-10-01" || pts[1].Date != "2026-10-01" {
		t.Fatalf("mixed/future observations: %+v", pts)
	}
}

func TestFREDCacheAndStaleness(t *testing.T) {
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "unavailable", 503)
			return
		}
		fmt.Fprint(w, `{"observations":[{"date":"2026-10-02","value":"4"}]}`)
	}))
	defer server.Close()
	cache := filepath.Join(t.TempDir(), "cache", "rates.json")
	pts, source, err := loadTreasuryRatesWith(context.Background(), []float64{45}, server.Client(), server.URL, "dummy", cache, rateTestNow())
	if err != nil || source != "FRED" || len(pts) != 2 {
		t.Fatalf("fetch: %s %v", source, err)
	}
	data, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "dummy") {
		t.Fatal("cache contains key")
	}
	fail = true
	_, source, err = loadTreasuryRatesWith(context.Background(), []float64{45}, server.Client(), server.URL, "dummy", cache, rateTestNow())
	if err != nil || !strings.Contains(source, "cached") {
		t.Fatalf("cache fallback %s %v", source, err)
	}
	_, _, err = loadTreasuryRatesWith(context.Background(), []float64{45}, server.Client(), server.URL, "dummy", cache, rateTestNow().AddDate(0, 0, 10))
	if err == nil {
		t.Fatal("accepted stale cache")
	}
	_, _, err = loadTreasuryRatesWith(context.Background(), []float64{365}, server.Client(), server.URL, "dummy", cache, rateTestNow())
	if err == nil {
		t.Fatal("accepted missing cache tenor")
	}
}

type rateErrorTransport struct{}

func (rateErrorTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("failed request %s", r.URL)
}
func TestFREDErrorsDoNotLeakCredentials(t *testing.T) {
	tenors, _ := requiredTenors([]float64{7})
	_, err := fetchTreasuryCurve(context.Background(), &http.Client{Transport: rateErrorTransport{}}, "https://example.invalid", "private-test-key", tenors, rateTestNow())
	if err == nil || strings.Contains(err.Error(), "private-test-key") {
		t.Fatalf("unsafe transport error: %v", err)
	}
	for _, body := range []string{`bad-json`, `{"observations":[]}`, `{"observations":[{"date":"2020-01-01","value":"4"}]}`, `{"observations":[{"date":"2026-10-02","value":"NaN"}]}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
		_, err := fetchTreasuryCurve(context.Background(), server.Client(), server.URL, "dummy", tenors, rateTestNow())
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid response: %s", body)
		}
	}
}

func TestRatePromptAutoManualAndFailure(t *testing.T) {
	loader := func(ctx context.Context, days []float64) ([]RatePoint, string, error) {
		return []RatePoint{{"DGS1MO", 1.0 / 12, 4, "2026-10-02"}, {"DGS3MO", .25, 5, "2026-10-02"}}, "cached FRED (live fetch unavailable)", nil
	}
	for _, tc := range []struct {
		input  string
		manual bool
	}{{"\nuse\n", false}, {"manual\n3.25\n", true}, {"FRED\nmanual\n3.25\n", true}} {
		sim := &MonteCarlo{ExpirationDays: []float64{7, 45}}
		var out bytes.Buffer
		err := configureRates(context.Background(), bufio.NewReader(strings.NewReader(tc.input)), &out, sim, loader)
		if err != nil {
			t.Fatal(err)
		}
		if tc.manual {
			if sim.Rates[0].Rate != .0325 || sim.Rates[1].Source != "manual" {
				t.Fatal("override failed")
			}
		} else {
			if sim.Rates[0].Rate == sim.Rates[1].Rate || !strings.Contains(out.String(), "2026-10-02") || !strings.Contains(out.String(), "cached") {
				t.Fatal("missing curve or provenance")
			}
		}
	}
	sim := &MonteCarlo{ExpirationDays: []float64{7}}
	fail := func(context.Context, []float64) ([]RatePoint, string, error) {
		return nil, "", fmt.Errorf("unavailable")
	}
	var out bytes.Buffer
	err := configureRates(context.Background(), bufio.NewReader(strings.NewReader("\n\n4\n")), &out, sim, fail)
	if err != nil || sim.RiskFreeRate != .04 || !strings.Contains(out.String(), "Automatic rates unavailable") {
		t.Fatalf("manual fallback %v %s", err, out.String())
	}
}

func TestRateCacheRejectsMalformedMetadata(t *testing.T) {
	tenors, _ := requiredTenors([]float64{7})
	for _, p := range []RatePoint{{"DGS1MO", 1.0 / 12, 4, "2026-10-10"}, {"DGS1MO", 1, 4, "2026-10-02"}, {"OTHER", 1.0 / 12, 4, "2026-10-02"}} {
		data, _ := json.Marshal([]RatePoint{p})
		var parsed []RatePoint
		json.Unmarshal(data, &parsed)
		if validateRatePoints(parsed, tenors, rateTestNow()) == nil {
			t.Fatalf("accepted cache %+v", p)
		}
	}
}
