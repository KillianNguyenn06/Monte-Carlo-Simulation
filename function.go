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

type PricingInput struct {
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
	Price          float64
	StandardError  float64
	ExecutionTime  float64
	SimulatedPaths int
	Seed           int64
}

func PriceOption(input PricingInput) (PricingResult, error) {
	input.ContractType = strings.ToUpper(strings.TrimSpace(input.ContractType))
	input.ExerciseStyle = strings.ToUpper(strings.TrimSpace(input.ExerciseStyle))
	if err := validatePricingInput(input); err != nil {
		return PricingResult{}, err
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
	if !isFinite(input.Rate) || input.Rate <= -1 {
		return fmt.Errorf("risk-free rate must be finite and greater than -100%%")
	}
	if !isFinite(input.DividendYield) || input.DividendYield < 0 {
		return fmt.Errorf("dividend yield must be finite and nonnegative")
	}
	if !isFinite(input.TimeYears) || input.TimeYears < 0 {
		return fmt.Errorf("time to expiration must be finite and nonnegative")
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
	if input.ContractType != callContract && input.ContractType != putContract {
		return fmt.Errorf("contract type must be CALL or PUT")
	}
	if input.ExerciseStyle != americanStyle && input.ExerciseStyle != europeanStyle {
		return fmt.Errorf("exercise style must be AMERICAN or EUROPEAN")
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

func priceAmericanLSM(input PricingInput) (PricingResult, error) {
	paths, err := assetPriceSim(input)
	if err != nil {
		return PricingResult{}, err
	}
	dt := input.TimeYears / float64(input.Steps)
	cashflows := make([]float64, input.Simulations)
	exerciseStep := make([]int, input.Simulations)
	europeanValues := make([]float64, input.Simulations)
	terminalDiscount := math.Exp(-input.Rate * input.TimeYears)

	for pathIndex := range paths {
		terminalPayoff := intrinsicValue(paths[pathIndex][input.Steps], input.Strike, input.ContractType)
		cashflows[pathIndex] = terminalPayoff
		exerciseStep[pathIndex] = input.Steps
		europeanValues[pathIndex] = terminalDiscount * terminalPayoff
	}

	for step := input.Steps - 1; step >= 1; step-- {
		x := make([]float64, 0, input.Simulations)
		y := make([]float64, 0, input.Simulations)
		indices := make([]int, 0, input.Simulations)
		for pathIndex := range paths {
			spot := paths[pathIndex][step]
			if intrinsicValue(spot, input.Strike, input.ContractType) <= 0 {
				continue
			}
			discountToStep := math.Exp(-input.Rate * dt * float64(exerciseStep[pathIndex]-step))
			x = append(x, spot/input.Spot)
			y = append(y, cashflows[pathIndex]*discountToStep)
			indices = append(indices, pathIndex)
		}
		if len(indices) == 0 {
			continue
		}

		coefficients, fitted := fitQuadratic(x, y)
		fallbackContinuation, _ := meanAndStandardError(y)
		for position, pathIndex := range indices {
			normalizedSpot := x[position]
			continuation := fallbackContinuation
			if fitted {
				continuation = coefficients[0] + coefficients[1]*normalizedSpot + coefficients[2]*normalizedSpot*normalizedSpot
			}
			continuation = math.Max(continuation, 0)
			immediateExercise := intrinsicValue(paths[pathIndex][step], input.Strike, input.ContractType)
			if immediateExercise > continuation {
				cashflows[pathIndex] = immediateExercise
				exerciseStep[pathIndex] = step
			}
		}
	}

	americanValues := make([]float64, input.Simulations)
	for pathIndex := range cashflows {
		americanValues[pathIndex] = cashflows[pathIndex] * math.Exp(-input.Rate*dt*float64(exerciseStep[pathIndex]))
	}
	americanPrice, americanSE := meanAndStandardError(americanValues)
	europeanPrice, europeanSE := meanAndStandardError(europeanValues)
	intrinsic := intrinsicValue(input.Spot, input.Strike, input.ContractType)

	result := PricingResult{
		Price:          americanPrice,
		StandardError:  americanSE,
		SimulatedPaths: input.Simulations,
	}
	// LSM is a lower-bound estimator. Enforce basic no-arbitrage lower bounds.
	if europeanPrice > result.Price {
		result.Price = europeanPrice
		result.StandardError = europeanSE
	}
	if intrinsic > result.Price {
		result.Price = intrinsic
		result.StandardError = 0
	}
	return result, nil
}

func fitQuadratic(x, y []float64) ([3]float64, bool) {
	if len(x) != len(y) || len(x) < 3 {
		return [3]float64{}, false
	}
	var sx, sx2, sx3, sx4 float64
	var sy, sxy, sx2y float64
	for i := range x {
		x2 := x[i] * x[i]
		sx += x[i]
		sx2 += x2
		sx3 += x2 * x[i]
		sx4 += x2 * x2
		sy += y[i]
		sxy += x[i] * y[i]
		sx2y += x2 * y[i]
	}
	matrix := [3][4]float64{
		{float64(len(x)), sx, sx2, sy},
		{sx, sx2, sx3, sxy},
		{sx2, sx3, sx4, sx2y},
	}

	for column := 0; column < 3; column++ {
		pivot := column
		for row := column + 1; row < 3; row++ {
			if math.Abs(matrix[row][column]) > math.Abs(matrix[pivot][column]) {
				pivot = row
			}
		}
		if math.Abs(matrix[pivot][column]) < 1e-12 {
			return [3]float64{}, false
		}
		matrix[column], matrix[pivot] = matrix[pivot], matrix[column]
		pivotValue := matrix[column][column]
		for entry := column; entry < 4; entry++ {
			matrix[column][entry] /= pivotValue
		}
		for row := 0; row < 3; row++ {
			if row == column {
				continue
			}
			factor := matrix[row][column]
			for entry := column; entry < 4; entry++ {
				matrix[row][entry] -= factor * matrix[column][entry]
			}
		}
	}
	return [3]float64{matrix[0][3], matrix[1][3], matrix[2][3]}, true
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

func writeHeatMapCSV(filename string, sim *MonteCarlo, prices, standardErrors [][]float64) error {
	if len(prices) != len(sim.StrikePrice) || len(standardErrors) != len(sim.StrikePrice) {
		return fmt.Errorf("price grids do not match strike count")
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
	})
	for rowIndex, strike := range sim.StrikePrice {
		if len(prices[rowIndex]) != len(sim.ExpirationDays) || len(standardErrors[rowIndex]) != len(sim.ExpirationDays) {
			return fmt.Errorf("price grid row %d does not match expiration count", rowIndex)
		}
		for columnIndex, days := range sim.ExpirationDays {
			rows = append(rows, []string{
				strconv.FormatFloat(strike, 'f', 2, 64),
				strconv.FormatFloat(days, 'f', 0, 64),
				strconv.FormatFloat(prices[rowIndex][columnIndex], 'f', 6, 64),
				strconv.FormatFloat(standardErrors[rowIndex][columnIndex], 'f', 6, 64),
				sim.CallOrPut,
				sim.ExerciseStyle,
				strconv.FormatFloat(sim.DividendYield, 'f', 8, 64),
				strconv.FormatFloat(sim.RiskFreeRate, 'f', 8, 64),
				strconv.FormatFloat(sim.Volatility, 'f', 8, 64),
				strconv.FormatInt(sim.Seed, 10),
			})
		}
	}
	return writeCSV(filename, rows)
}

func writeAssetPriceCSV(filename string, paths [][]float64) error {
	if len(paths) == 0 || len(paths[0]) == 0 {
		return fmt.Errorf("asset-price grid is empty")
	}
	columns := len(paths[0])
	rows := make([][]string, 0, len(paths)+1)
	header := make([]string, columns)
	for step := range header {
		header[step] = fmt.Sprintf("Step_%d", step)
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
