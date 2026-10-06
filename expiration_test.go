package main

import (
	"bufio"
	"bytes"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExpirationParsingAndDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	local, err := parseExpiration("2026-10-06 16:00", ny)
	if err != nil {
		t.Fatal(err)
	}
	offset, err := parseExpiration("2026-10-06T13:00:00-07:00", ny)
	if err != nil || !local.Equal(offset) {
		t.Fatal("offsets represent different instants")
	}
	for _, value := range []string{"2026-03-08 02:30", "2026-11-01 01:30", "2026-10-06", "2026-02-30 16:00"} {
		if _, err := parseExpiration(value, ny); err == nil {
			t.Fatalf("accepted ambiguous/invalid time %s", value)
		}
	}
	if _, err := parseExpiration("2026-11-01T01:30:00-04:00", ny); err != nil {
		t.Fatal(err)
	}
}

func TestExpirationPromptRejectsPastAndPreservesFractions(t *testing.T) {
	valuation := time.Date(2026, 10, 6, 19, 0, 0, 0, time.UTC)
	sim := &MonteCarlo{ValuationTime: valuation}
	var out bytes.Buffer
	err := collectExpiration(bufio.NewReader(strings.NewReader("0\n14:00\n2026-10-07 16:00\n16:00\n")), &out, sim)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "precedes") || sim.ExpirationDays[0] != 1.0/24 || sim.ExpirationDays[4] != 28+2.0/24 {
		t.Fatalf("wrong durations %+v", sim.ExpirationDays)
	}
	if formatDTE(sim.ExpirationDays[0]) != "1.00h" || effectiveTimeYears(0) != 0 {
		t.Fatal("0DTE conversion is inaccurate")
	}
	if remainingDays(valuation, valuation.Add(-time.Hour)) != 0 || remainingDays(valuation, valuation) != 0 {
		t.Fatal("expired duration must be zero")
	}
}

func TestPositiveDTERequiresNoTimeInput(t *testing.T) {
	sim := &MonteCarlo{ValuationTime: time.Date(2026, 10, 6, 19, 0, 0, 0, time.UTC)}
	var output bytes.Buffer
	reader := bufio.NewReader(strings.NewReader("7\nCALL\n"))
	if err := collectExpiration(reader, &output, sim); err != nil {
		t.Fatal(err)
	}
	next, err := readLine(reader)
	if err != nil || next != "CALL" {
		t.Fatal("positive DTE consumed another input")
	}
	if sim.ExpirationDays[0] != 7 || sim.ExpirationDays[4] != 35 || len(sim.Expirations) != 0 {
		t.Fatal("whole-day grid changed")
	}
	if strings.Contains(output.String(), "HH:MM") {
		t.Fatal("positive DTE asked for a time")
	}
}

func TestWeeklyExpirationDST(t *testing.T) {
	sim := &MonteCarlo{ValuationTime: time.Date(2026, 10, 25, 19, 0, 0, 0, time.UTC)}
	err := collectExpiration(bufio.NewReader(strings.NewReader("0\n16:00\n")), &bytes.Buffer{}, sim)
	if err != nil {
		t.Fatal(err)
	}
	if sim.Expirations[1].Hour() != 16 || sim.Expirations[1].Sub(sim.Expirations[0]) != 169*time.Hour {
		t.Fatal("weekly wall time not preserved across DST")
	}
}

func TestExpiredOptionsReturnExactPayoffsWithoutSimulation(t *testing.T) {
	for _, style := range []string{americanStyle, europeanStyle} {
		for _, contract := range []string{callContract, putContract} {
			for _, spot := range []float64{80, 100, 120} {
				p := PricingInput{Spot: spot, Strike: 100, ContractType: contract, ExerciseStyle: style, TimeYears: 0}
				r, err := PriceOption(p)
				if err != nil {
					t.Fatal(err)
				}
				want := intrinsicValue(spot, 100, contract)
				if r.Price != want || r.StandardError != 0 || r.ConfidenceLow != want || r.ConfidenceHigh != want || r.SimulatedPaths != 0 || r.TrainingPaths != 0 {
					t.Fatalf("incorrect expiration result %+v", r)
				}
			}
		}
	}
	p := testPricingInput()
	p.TimeYears = -.1
	if _, err := PriceOption(p); err == nil {
		t.Fatal("negative engine horizon must be rejected")
	}
}

func TestFractionalExpirationCSVAndGrid(t *testing.T) {
	start := time.Date(2026, 10, 6, 19, 0, 0, 0, time.UTC)
	sim := &MonteCarlo{ValuationTime: start, Expirations: []time.Time{start, start.Add(time.Hour)}, UnderlyingPrice: 100, StrikePrice: []float64{100}, ExpirationDays: []float64{0, 1.0 / 24}, CallOrPut: callContract, ExerciseStyle: americanStyle, Volatility: .2, Simulation: 2000, TrainingPaths: 3000, Seed: 42}
	var out bytes.Buffer
	grids, err := displayOption(sim, &out)
	if err != nil {
		t.Fatal(err)
	}
	if grids.Price[0][0] != 0 || grids.Price[0][1] <= 0 || !strings.Contains(out.String(), "1.00h") || strings.Contains(out.String(), "NaN") {
		t.Fatal("same-day value or expired diagnostics incorrect")
	}
	path := filepath.Join(t.TempDir(), "times.csv")
	if err := writeHeatMapCSV(path, sim, grids.Price, grids.StandardError, grids.Details); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, path)
	cols := map[string]int{}
	for i, h := range rows[0] {
		cols[h] = i
	}
	days, err := strconv.ParseFloat(rows[2][cols["Expiration_Y"]], 64)
	if err != nil || math.Abs(days-1.0/24) > 1e-15 {
		t.Fatal("fractional expiration rounded away")
	}
	if rows[2][cols["ExpirationTimestamp"]] != timestampString(start.Add(time.Hour)) || rows[2][cols["ValuationTimestamp"]] != timestampString(start) {
		t.Fatal("missing timestamp provenance")
	}
}
