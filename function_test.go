package main

import (
	"encoding/csv"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
)

func TestParkinsonVolatility(t *testing.T) {
	high, low := 110.0, 100.0
	got, err := parkinsonVolatility(high, low)
	if err != nil {
		t.Fatalf("parkinsonVolatility returned an error: %v", err)
	}
	want := math.Sqrt(math.Pow(math.Log(high/low), 2) / (4 * math.Log(2)) * 252)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("parkinsonVolatility = %v, want %v", got, want)
	}
}

func TestParkinsonVolatilityRejectsInvalidRange(t *testing.T) {
	for _, testCase := range []struct {
		high float64
		low  float64
	}{
		{high: 100, low: 0},
		{high: 100, low: 100},
		{high: 90, low: 100},
		{high: math.Inf(1), low: 100},
	} {
		if _, err := parkinsonVolatility(testCase.high, testCase.low); err == nil {
			t.Fatalf("expected an error for high=%v low=%v", testCase.high, testCase.low)
		}
	}
}

func TestEuropeanPricingIsDeterministicAcrossWorkerCounts(t *testing.T) {
	input := testPricingInput()
	input.ExerciseStyle = europeanStyle
	input.Workers = 1
	oneWorker, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Workers = 4
	fourWorkers, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	if oneWorker.Price != fourWorkers.Price || oneWorker.StandardError != fourWorkers.StandardError {
		t.Fatalf("worker count changed deterministic result: %+v vs %+v", oneWorker, fourWorkers)
	}
	if oneWorker.SimulatedPaths != input.Simulations {
		t.Fatalf("simulated %d paths, want %d", oneWorker.SimulatedPaths, input.Simulations)
	}
}

func TestEuropeanCallAgainstBlackScholes(t *testing.T) {
	input := testPricingInput()
	input.ExerciseStyle = europeanStyle
	input.Simulations = 50_000
	result, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	want := blackScholes(input)
	tolerance := 4*result.StandardError + 0.05
	if math.Abs(result.Price-want) > tolerance {
		t.Fatalf("European Monte Carlo price %.4f differs from Black-Scholes %.4f by more than %.4f", result.Price, want, tolerance)
	}
}

func TestAmericanPutRespectsLowerBounds(t *testing.T) {
	input := testPricingInput()
	input.ContractType = putContract
	input.ExerciseStyle = americanStyle
	input.Spot = 80
	input.Strike = 100
	input.Simulations = 8_000
	input.Steps = 50
	result, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	intrinsic := intrinsicValue(input.Spot, input.Strike, input.ContractType)
	if result.Price < intrinsic {
		t.Fatalf("American put price %.4f is below intrinsic value %.4f", result.Price, intrinsic)
	}
	if result.SimulatedPaths != input.Simulations {
		t.Fatalf("simulated %d paths, want %d", result.SimulatedPaths, input.Simulations)
	}
}

func TestAmericanPutAgainstBinomialBenchmark(t *testing.T) {
	input := testPricingInput()
	input.ContractType = putContract
	input.ExerciseStyle = americanStyle
	input.DividendYield = 0
	input.Simulations = 40_000
	input.Steps = 50
	result, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	want := americanBinomial(input, 1_000)
	if difference := math.Abs(result.Price - want); difference > 0.75 {
		t.Fatalf("American LSM price %.4f differs from binomial benchmark %.4f by %.4f", result.Price, want, difference)
	}
}

func TestAmericanCallWithoutDividendApproximatesEuropeanValue(t *testing.T) {
	input := testPricingInput()
	input.ExerciseStyle = americanStyle
	input.DividendYield = 0
	input.Simulations = 30_000
	input.Steps = 50
	result, err := PriceOption(input)
	if err != nil {
		t.Fatal(err)
	}
	want := blackScholes(input)
	if difference := math.Abs(result.Price - want); difference > 0.60 {
		t.Fatalf("American non-dividend call %.4f differs from European benchmark %.4f by %.4f", result.Price, want, difference)
	}
}

func TestAssetPathsAreDeterministicAcrossWorkerCounts(t *testing.T) {
	input := testPricingInput()
	input.Simulations = 12
	input.Steps = 8
	input.Workers = 1
	oneWorker, err := assetPriceSim(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Workers = 6
	manyWorkers, err := assetPriceSim(input)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(oneWorker, manyWorkers) {
		t.Fatal("asset paths changed with worker count")
	}
}

func TestFitQuadratic(t *testing.T) {
	x := []float64{0, 1, 2, 3, 4}
	y := make([]float64, len(x))
	for index := range x {
		y[index] = 2 + 3*x[index] + 4*x[index]*x[index]
	}
	coefficients, ok := fitQuadratic(x, y)
	if !ok {
		t.Fatal("fitQuadratic did not produce a fit")
	}
	want := [3]float64{2, 3, 4}
	for index := range want {
		if math.Abs(coefficients[index]-want[index]) > 1e-10 {
			t.Fatalf("coefficient %d = %v, want %v", index, coefficients[index], want[index])
		}
	}
}

func TestParallelForRunsEveryIndexExactlyOnce(t *testing.T) {
	counts := make([]atomic.Int32, 2)
	parallelFor(len(counts), 64, func(index int) {
		counts[index].Add(1)
	})
	for index := range counts {
		if got := counts[index].Load(); got != 1 {
			t.Fatalf("index %d executed %d times, want 1", index, got)
		}
	}
}

func TestCSVWriters(t *testing.T) {
	directory := t.TempDir()
	sim := &MonteCarlo{
		StrikePrice:    []float64{90, 100},
		ExpirationDays: []float64{7, 14, 21},
		CallOrPut:      callContract,
		ExerciseStyle:  americanStyle,
		DividendYield:  0.01,
		RiskFreeRate:   0.05,
		Volatility:     0.20,
		Seed:           42,
	}
	prices := [][]float64{{12, 13, 14}, {6, 7, 8}}
	standardErrors := [][]float64{{0.1, 0.2, 0.3}, {0.4, 0.5, 0.6}}
	heatmapPath := filepath.Join(directory, "heatmap.csv")
	if err := writeHeatMapCSV(heatmapPath, sim, prices, standardErrors); err != nil {
		t.Fatal(err)
	}
	rows := readCSV(t, heatmapPath)
	if len(rows) != 7 || len(rows[0]) != 10 {
		t.Fatalf("unexpected heatmap dimensions: %d x %d", len(rows), len(rows[0]))
	}
	if rows[0][3] != "StandardError" || rows[6][2] != "8.000000" || rows[6][5] != americanStyle {
		t.Fatalf("unexpected heatmap contents: %v", rows)
	}

	assetPath := filepath.Join(directory, "assets.csv")
	if err := writeAssetPriceCSV(assetPath, [][]float64{{100, 101, 102}, {100, 99, 98}}); err != nil {
		t.Fatal(err)
	}
	assetRows := readCSV(t, assetPath)
	if len(assetRows) != 3 || len(assetRows[0]) != 3 || assetRows[0][2] != "Step_2" {
		t.Fatalf("unexpected asset CSV: %v", assetRows)
	}
}

func TestExpirationAndStrikeGrids(t *testing.T) {
	if got, want := expirationDate(10), []float64{10, 17, 24, 31, 38}; !reflect.DeepEqual(got, want) {
		t.Fatalf("expirationDate = %v, want %v", got, want)
	}
	got, want := strikePrice(100), []float64{90, 95, 100, 105, 110}
	for index := range want {
		if math.Abs(got[index]-want[index]) > 1e-12 {
			t.Fatalf("strikePrice = %v, want %v", got, want)
		}
	}
}

func TestInvalidPricingInput(t *testing.T) {
	input := testPricingInput()
	input.Simulations = 0
	if _, err := PriceOption(input); err == nil {
		t.Fatal("expected zero simulations to fail")
	}
	input = testPricingInput()
	input.ContractType = "UNKNOWN"
	if _, err := PriceOption(input); err == nil {
		t.Fatal("expected invalid contract type to fail")
	}
}

func testPricingInput() PricingInput {
	return PricingInput{
		Spot:          100,
		Strike:        100,
		Rate:          0.05,
		DividendYield: 0.01,
		TimeYears:     1,
		Volatility:    0.20,
		Steps:         50,
		Simulations:   10_000,
		ContractType:  callContract,
		ExerciseStyle: europeanStyle,
		Seed:          42,
		Workers:       4,
	}
}

func blackScholes(input PricingInput) float64 {
	d1 := (math.Log(input.Spot/input.Strike) + (input.Rate-input.DividendYield+0.5*input.Volatility*input.Volatility)*input.TimeYears) /
		(input.Volatility * math.Sqrt(input.TimeYears))
	d2 := d1 - input.Volatility*math.Sqrt(input.TimeYears)
	if input.ContractType == callContract {
		return input.Spot*math.Exp(-input.DividendYield*input.TimeYears)*normalCDF(d1) -
			input.Strike*math.Exp(-input.Rate*input.TimeYears)*normalCDF(d2)
	}
	return input.Strike*math.Exp(-input.Rate*input.TimeYears)*normalCDF(-d2) -
		input.Spot*math.Exp(-input.DividendYield*input.TimeYears)*normalCDF(-d1)
}

func americanBinomial(input PricingInput, steps int) float64 {
	dt := input.TimeYears / float64(steps)
	up := math.Exp(input.Volatility * math.Sqrt(dt))
	down := 1 / up
	discount := math.Exp(-input.Rate * dt)
	probability := (math.Exp((input.Rate-input.DividendYield)*dt) - down) / (up - down)
	values := make([]float64, steps+1)
	for upMoves := 0; upMoves <= steps; upMoves++ {
		spot := input.Spot * math.Pow(up, float64(upMoves)) * math.Pow(down, float64(steps-upMoves))
		values[upMoves] = intrinsicValue(spot, input.Strike, input.ContractType)
	}
	for step := steps - 1; step >= 0; step-- {
		for upMoves := 0; upMoves <= step; upMoves++ {
			spot := input.Spot * math.Pow(up, float64(upMoves)) * math.Pow(down, float64(step-upMoves))
			continuation := discount * (probability*values[upMoves+1] + (1-probability)*values[upMoves])
			values[upMoves] = math.Max(intrinsicValue(spot, input.Strike, input.ContractType), continuation)
		}
	}
	return values[0]
}

func normalCDF(value float64) float64 {
	return 0.5 * (1 + math.Erf(value/math.Sqrt2))
}

func readCSV(t *testing.T, filename string) [][]string {
	t.Helper()
	file, err := os.Open(filename)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
