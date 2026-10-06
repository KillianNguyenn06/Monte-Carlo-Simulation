package main

import (
	"bytes"
	"math"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGridRateAndDiagnosticExport(t *testing.T) {
	sim := &MonteCarlo{UnderlyingPrice: 100, StrikePrice: []float64{95, 105}, ExpirationDays: []float64{7, 35}, CallOrPut: putContract, ExerciseStyle: americanStyle, Volatility: .2, Simulation: 1000, TrainingPaths: 2000, Seed: 42, Workers: 2, Rates: []RateSelection{{.03, "FRED DGS1MO", "2026-10-02", rateConvention}, {.04, "FRED DGS1MO+DGS3MO", "2026-10-02", rateConvention}}}
	var output bytes.Buffer
	result, err := displayOption(sim, &output)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Rate for") || strings.Contains(output.String(), "frozen policy") || !strings.Contains(output.String(), "Training Path per contract: 2000") || !strings.Contains(output.String(), "95% interval") {
		t.Fatal("unexpected summary layout")
	}
	seen := map[int64]bool{}
	for row := range result.Details {
		for col, r := range result.Details[row] {
			if seen[r.TrainingSeed] || seen[r.ValuationSeed] {
				t.Fatal("stream reused across contracts")
			}
			seen[r.TrainingSeed], seen[r.ValuationSeed] = true, true
			p := PricingInput{Spot: 100, Strike: sim.StrikePrice[row], Rate: sim.Rates[col].Rate, TimeYears: effectiveTimeYears(sim.ExpirationDays[col]), Volatility: .2, Steps: stepsForDays(sim.ExpirationDays[col]), Simulations: 1000, TrainingPaths: 2000, Seed: deriveSeed(42, row*2+col), Workers: 2, ContractType: putContract, ExerciseStyle: americanStyle}
			direct, err := PriceOption(p)
			if err != nil {
				t.Fatal(err)
			}
			if direct.Price != r.Price {
				t.Fatal("grid used wrong rate")
			}
		}
	}
	path := filepath.Join(t.TempDir(), "grid.csv")
	if err := writeHeatMapCSV(path, sim, result.Price, result.StandardError, result.Details); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, path)
	columns := map[string]int{}
	for i, name := range rows[0] {
		columns[name] = i
	}
	for i, row := range rows[1:] {
		col := i % 2
		rate, err := strconv.ParseFloat(row[columns["RiskFreeRate"]], 64)
		if err != nil || rate != sim.Rates[col].Rate {
			t.Fatal("wrong exported rate")
		}
		if row[columns["RateObservationDate"]] != "2026-10-02" || row[columns["TrainingPaths"]] != "2000" || row[columns["ValuationPaths"]] != "1000" {
			t.Fatal("missing export metadata")
		}
		raw, err := strconv.ParseFloat(row[columns["RawPolicyPrice"]], 64)
		if err != nil || math.Abs(raw-result.Details[i/2][col].Price) > 1e-12 {
			t.Fatal("wrong raw price")
		}
	}
}
