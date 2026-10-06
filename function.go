package main

import (
	"encoding/csv"
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	americanStyle      = "AMERICAN"
	europeanStyle      = "EUROPEAN"
	callContract       = "CALL"
	putContract        = "PUT"
	tradingDaysPerYear = 252
)

const defaultTrainingPaths = 50_000

type PricingInput struct {
	TrainingPaths int   // Zero selects defaultTrainingPaths; separate from valuation count.
	TrainingSeed  int64 // Zero derives a separate stream from Seed.
	ValuationSeed int64 // Zero derives a separate stream from Seed.
	Spot          float64
	Strike        float64
	Rate          float64
	DividendYield float64
	TimeYears     float64
	Volatility    float64
	Steps         int
	Simulations   int
	ContractType  string
	ExerciseStyle string
	Seed          int64
	Workers       int
}

type PricingResult struct {
	TrainingPaths       int
	TrainingSeed        int64
	ValuationSeed       int64
	ConfidenceLow       float64
	ConfidenceHigh      float64
	EuropeanPrice       float64
	Intrinsic           float64
	BoundAdjustedPrice  float64 // Diagnostic only; Price and its SE remain unadjusted.
	BoundAdjustment     float64
	FallbackRegressions int
	NoITMSteps          int
	ExerciseCounts      []int
	Price               float64
	StandardError       float64
	ExecutionTime       float64
	SimulatedPaths      int
	Seed                int64
}

func PriceOption(input PricingInput) (PricingResult, error) {
	input.ContractType = strings.ToUpper(strings.TrimSpace(input.ContractType))
	input.ExerciseStyle = strings.ToUpper(strings.TrimSpace(input.ExerciseStyle))
	if err := validatePricingInput(input); err != nil {
		return PricingResult{}, err
	}
	// At expiry there is no Monte Carlo uncertainty or exercise strategy to learn.
	if input.TimeYears == 0 {
		payoff := intrinsicValue(input.Spot, input.Strike, input.ContractType)
		return PricingResult{Price: payoff, ConfidenceLow: payoff, ConfidenceHigh: payoff, EuropeanPrice: payoff, Intrinsic: payoff, BoundAdjustedPrice: payoff, Seed: input.Seed}, nil
	}
	if input.Seed == 0 {
		input.Seed = time.Now().UnixNano()
	}

	start := time.Now()
	var result PricingResult
	var err error
	if input.ExerciseStyle == americanStyle {
		result, err = priceAmericanLSM(input)
	} else {
		result, err = priceEuropean(input)
	}
	if err == nil {
		result.ConfidenceLow = result.Price - 1.959963984540054*result.StandardError
		result.ConfidenceHigh = result.Price + 1.959963984540054*result.StandardError
	}
	result.ExecutionTime = float64(time.Since(start).Microseconds()) / 1_000
	result.Seed = input.Seed
	return result, err
}

func validatePricingInput(input PricingInput) error {
	if !isFinitePositive(input.Spot) {
		return fmt.Errorf("spot price must be finite and greater than zero")
	}
	if !isFinitePositive(input.Strike) {
		return fmt.Errorf("strike price must be finite and greater than zero")
	}
	if !isFinite(input.TimeYears) || input.TimeYears < 0 {
		return fmt.Errorf("time to expiration must be finite and nonnegative")
	}
	if input.ContractType != callContract && input.ContractType != putContract {
		return fmt.Errorf("contract type must be CALL or PUT")
	}
	if input.ExerciseStyle != americanStyle && input.ExerciseStyle != europeanStyle {
		return fmt.Errorf("exercise style must be AMERICAN or EUROPEAN")
	}
	if input.TimeYears == 0 {
		return nil
	}
	if !isFinite(input.Rate) || input.Rate <= -1 {
		return fmt.Errorf("risk-free rate must be finite and greater than -100%%")
	}
	if !isFinite(input.DividendYield) || input.DividendYield < 0 {
		return fmt.Errorf("dividend yield must be finite and nonnegative")
	}

	if !isFinitePositive(input.Volatility) {
		return fmt.Errorf("volatility must be finite and greater than zero")
	}
	if input.Steps <= 0 {
		return fmt.Errorf("steps must be greater than zero")
	}
	if input.Simulations <= 0 {
		return fmt.Errorf("simulations must be greater than zero")
	}
	if input.TrainingPaths < 0 {
		return fmt.Errorf("training path count must be nonnegative (zero selects the default)")
	}
	if input.ExerciseStyle == americanStyle && (input.Simulations < 2 || input.TrainingPaths == 1 || input.TrainingPaths == 2) {
		return fmt.Errorf("American pricing requires at least 2 valuation paths and 3 training paths")
	}

	return nil
}

func priceEuropean(input PricingInput) (PricingResult, error) {
	values := make([]float64, input.Simulations)
	discount := math.Exp(-input.Rate * input.TimeYears)
	drift := (input.Rate - input.DividendYield - 0.5*input.Volatility*input.Volatility) * input.TimeYears
	diffusion := input.Volatility * math.Sqrt(input.TimeYears)

	parallelFor(input.Simulations, input.Workers, func(index int) {
		rng := rand.New(rand.NewSource(deriveSeed(input.Seed, index)))
		terminalPrice := input.Spot * math.Exp(drift+diffusion*rng.NormFloat64())
		values[index] = discount * intrinsicValue(terminalPrice, input.Strike, input.ContractType)
	})

	price, standardError := meanAndStandardError(values)
	return PricingResult{
		Price:          price,
		StandardError:  standardError,
		SimulatedPaths: input.Simulations,
	}, nil
}

func intrinsicValue(spot, strike float64, contractType string) float64 {
	if contractType == callContract {
		return math.Max(spot-strike, 0)
	}
	return math.Max(strike-spot, 0)
}

func meanAndStandardError(values []float64) (float64, float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sum float64
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	if len(values) == 1 {
		return mean, 0
	}
	var squaredDifferences float64
	for _, value := range values {
		difference := value - mean
		squaredDifferences += difference * difference
	}
	variance := squaredDifferences / float64(len(values)-1)
	return mean, math.Sqrt(variance / float64(len(values)))
}

func expirationDate(start float64) []float64 {
	expirations := make([]float64, 5)
	for index := range expirations {
		expirations[index] = start + float64(index*7)
	}
	return expirations
}

func strikePrice(spot float64) []float64 {
	return []float64{spot * 0.90, spot * 0.95, spot, spot * 1.05, spot * 1.10}
}

func parkinsonVolatility(high, low float64) (float64, error) {
	if !isFinitePositive(high) || !isFinitePositive(low) || high <= low {
		return 0, fmt.Errorf("high and low must be finite positive values with high greater than low")
	}
	logRange := math.Log(high / low)
	dailyVariance := (logRange * logRange) / (4 * math.Log(2))
	return math.Sqrt(dailyVariance * 252), nil
}

func assetPriceSim(input PricingInput) ([][]float64, error) {
	if !isFinitePositive(input.Spot) {
		return nil, fmt.Errorf("spot price must be finite and greater than zero")
	}
	if !isFinite(input.Rate) || input.Rate <= -1 {
		return nil, fmt.Errorf("risk-free rate must be finite and greater than -100%%")
	}
	if !isFinite(input.DividendYield) || input.DividendYield < 0 {
		return nil, fmt.Errorf("dividend yield must be finite and nonnegative")
	}
	if !isFinite(input.TimeYears) || input.TimeYears < 0 {
		return nil, fmt.Errorf("time horizon must be finite and nonnegative")
	}
	if !isFinitePositive(input.Volatility) {
		return nil, fmt.Errorf("volatility must be finite and greater than zero")
	}
	if input.Steps <= 0 || input.Simulations <= 0 {
		return nil, fmt.Errorf("steps and path count must be greater than zero")
	}
	if input.Seed == 0 {
		input.Seed = time.Now().UnixNano()
	}

	dt := input.TimeYears / float64(input.Steps)
	drift := (input.Rate - input.DividendYield - 0.5*input.Volatility*input.Volatility) * dt
	diffusion := input.Volatility * math.Sqrt(dt)
	paths := make([][]float64, input.Simulations)
	parallelFor(input.Simulations, input.Workers, func(pathIndex int) {
		rng := rand.New(rand.NewSource(deriveSeed(input.Seed, pathIndex)))
		path := make([]float64, input.Steps+1)
		path[0] = input.Spot
		for step := 0; step < input.Steps; step++ {
			path[step+1] = path[step] * math.Exp(drift+diffusion*rng.NormFloat64())
		}
		paths[pathIndex] = path
	})
	return paths, nil
}

func parallelFor(total, requestedWorkers int, operation func(index int)) {
	workers := requestedWorkers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > total {
		workers = total
	}
	var waitGroup sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		start := worker * total / workers
		end := (worker + 1) * total / workers
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for index := start; index < end; index++ {
				operation(index)
			}
		}()
	}
	waitGroup.Wait()
}

func deriveSeed(base int64, index int) int64 {
	value := uint64(base) + 0x9e3779b97f4a7c15*uint64(index+1)
	value = (value ^ (value >> 30)) * 0xbf58476d1ce4e5b9
	value = (value ^ (value >> 27)) * 0x94d049bb133111eb
	value ^= value >> 31
	return int64(value)
}

func writeHeatMapCSV(filename string, sim *MonteCarlo, prices, standardErrors [][]float64, details ...[][]PricingResult) error {
	if len(prices) != len(sim.StrikePrice) || len(standardErrors) != len(sim.StrikePrice) {
		return fmt.Errorf("price grids do not match strike count")
	}
	if len(details) > 0 {
		if len(details[0]) != len(sim.StrikePrice) {
			return fmt.Errorf("diagnostics do not match strikes")
		}
		for _, row := range details[0] {
			if len(row) != len(sim.ExpirationDays) {
				return fmt.Errorf("diagnostics do not match expirations")
			}
		}
	}
	rows := make([][]string, 0, len(sim.StrikePrice)*len(sim.ExpirationDays)+1)
	rows = append(rows, []string{
		"Strike_X",
		"Expiration_Y",
		"OptionPrice_Z",
		"StandardError",
		"ContractType",
		"ExerciseStyle",
		"DividendYield",
		"RiskFreeRate",
		"Volatility",
		"Seed",
		"RateSource", "RateObservationDate", "RateConvention", "ValuationTimestamp", "ExpirationTimestamp",
		"TrainingPaths", "ValuationPaths", "TrainingSeed", "ValuationSeed",
		"RawPolicyPrice", "Conditional95Low", "Conditional95High",
		"BoundAdjustedPrice", "BoundAdjustment", "FallbackRegressions", "NoITMSteps", "ExerciseCounts",
	})
	for rowIndex, strike := range sim.StrikePrice {
		if len(prices[rowIndex]) != len(sim.ExpirationDays) || len(standardErrors[rowIndex]) != len(sim.ExpirationDays) {
			return fmt.Errorf("price grid row %d does not match expiration count", rowIndex)
		}
		for columnIndex, days := range sim.ExpirationDays {
			rate := sim.rateAt(columnIndex)
			r := PricingResult{}
			if len(details) > 0 {
				r = details[0][rowIndex][columnIndex]
			}
			diagnostic := func(v float64) string {
				if len(details) == 0 || sim.ExerciseStyle != americanStyle {
					return ""
				}
				return strconv.FormatFloat(v, 'g', 17, 64)
			}
			counts := make([]string, len(r.ExerciseCounts))
			for i, v := range r.ExerciseCounts {
				counts[i] = strconv.Itoa(v)
			}
			rows = append(rows, []string{
				strconv.FormatFloat(strike, 'f', 2, 64),
				strconv.FormatFloat(days, 'g', 17, 64),
				strconv.FormatFloat(prices[rowIndex][columnIndex], 'f', 6, 64),
				strconv.FormatFloat(standardErrors[rowIndex][columnIndex], 'f', 6, 64),
				sim.CallOrPut,
				sim.ExerciseStyle,
				strconv.FormatFloat(sim.DividendYield, 'f', 8, 64),
				strconv.FormatFloat(rate.Rate, 'g', 17, 64),
				strconv.FormatFloat(sim.Volatility, 'f', 8, 64),
				strconv.FormatInt(sim.Seed, 10),
				rate.Source, rate.ObservationDate, rate.Method, timestampString(sim.ValuationTime), sim.expirationString(columnIndex),
				strconv.Itoa(r.TrainingPaths), strconv.Itoa(r.SimulatedPaths), strconv.FormatInt(r.TrainingSeed, 10), strconv.FormatInt(r.ValuationSeed, 10),
				diagnostic(r.Price), diagnostic(r.ConfidenceLow), diagnostic(r.ConfidenceHigh), diagnostic(r.BoundAdjustedPrice), diagnostic(r.BoundAdjustment), strconv.Itoa(r.FallbackRegressions), strconv.Itoa(r.NoITMSteps), strings.Join(counts, ";"),
			})
		}
	}
	return writeCSV(filename, rows)
}

func writeAssetPriceCSV(filename string, paths [][]float64, horizonDays ...float64) error {
	if len(paths) == 0 || len(paths[0]) == 0 {
		return fmt.Errorf("asset-price grid is empty")
	}
	if len(horizonDays) > 1 || (len(horizonDays) == 1 && (!isFinitePositive(horizonDays[0]) || len(paths[0]) < 2)) {
		return fmt.Errorf("calendar horizon must be finite and positive with at least two path points")
	}
	columns := len(paths[0])
	rows := make([][]string, 0, len(paths)+1)
	header := make([]string, columns)
	for step := range header {
		header[step] = fmt.Sprintf("Step_%d", step)
		if len(horizonDays) == 1 {
			day := float64(step) * horizonDays[0] / float64(columns-1)
			header[step] = "Day_" + strconv.FormatFloat(day, 'g', 17, 64)
		}
	}
	rows = append(rows, header)
	for pathIndex, path := range paths {
		if len(path) != columns {
			return fmt.Errorf("asset-price path %d has %d columns; expected %d", pathIndex, len(path), columns)
		}
		row := make([]string, columns)
		for step, price := range path {
			row[step] = strconv.FormatFloat(price, 'f', 6, 64)
		}
		rows = append(rows, row)
	}
	return writeCSV(filename, rows)
}

func writeCSV(filename string, rows [][]string) error {
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("create %s: %w", filename, err)
	}
	writer := csv.NewWriter(file)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			_ = file.Close()
			return fmt.Errorf("write %s: %w", filename, err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		_ = file.Close()
		return fmt.Errorf("flush %s: %w", filename, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", filename, err)
	}
	return nil
}

func formatNumber(number float64) string {
	absolute := math.Abs(number)
	switch {
	case absolute >= 1_000_000_000_000:
		return fmt.Sprintf("%.2fT", number/1_000_000_000_000)
	case absolute >= 1_000_000_000:
		return fmt.Sprintf("%.2fB", number/1_000_000_000)
	case absolute >= 1_000_000:
		return fmt.Sprintf("%.2fM", number/1_000_000)
	case absolute >= 1_000:
		return fmt.Sprintf("%.2fK", number/1_000)
	default:
		return fmt.Sprintf("%.2f", number)
	}
}
