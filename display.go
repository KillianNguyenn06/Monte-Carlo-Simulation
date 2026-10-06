package main

import (
	"fmt"
	"io"
	"math"
	"text/tabwriter"
)

type PricingGrids struct {
	Details       [][]PricingResult
	Price         [][]float64
	StandardError [][]float64
	ExecutionTime [][]float64
}

func displayOption(sim *MonteCarlo, output io.Writer) (PricingGrids, error) {
	rows := len(sim.StrikePrice)
	columns := len(sim.ExpirationDays)
	results := PricingGrids{
		Details:       make([][]PricingResult, rows),
		Price:         makeGrid(rows, columns),
		StandardError: makeGrid(rows, columns),
		ExecutionTime: makeGrid(rows, columns),
	}

	fmt.Fprintln(output, "\n================ MONTE CARLO SIMULATION ================")
	fmt.Fprintf(output, "%-32sVolume: %s\n", "Symbol: "+sim.StockSymbol, formatNumber(sim.Volume))
	fmt.Fprintf(output, "%-32sExercise: %s\n", "Contract: "+sim.CallOrPut, sim.ExerciseStyle)
	fmt.Fprintf(output, "%-32s%-28sVolatility: %.3f%%\n",
		fmt.Sprintf("Spot: $%.2f", sim.UnderlyingPrice), fmt.Sprintf("Dividend: %.3f%%", sim.DividendYield*100), sim.Volatility*100)
	fmt.Fprintf(output, "Volatility source: %s\n", sim.VolatilitySource)
	fmt.Fprintf(output, "Paths per contract: %d\tSeed: %d\n", sim.Simulation, sim.Seed)

	if sim.ExerciseStyle == americanStyle {
		count := sim.TrainingPaths
		if count == 0 {
			count = defaultTrainingPaths
		}
		fmt.Fprintf(output, "Training Path per contract: %d\n", count)
	}
	for row, strike := range sim.StrikePrice {
		results.Details[row] = make([]PricingResult, columns)
		for column, days := range sim.ExpirationDays {
			input := PricingInput{
				Spot:          sim.UnderlyingPrice,
				Strike:        strike,
				Rate:          sim.rateAt(column).Rate,
				DividendYield: sim.DividendYield,
				TimeYears:     effectiveTimeYears(days),
				Volatility:    sim.Volatility,
				Steps:         stepsForDays(days),
				Simulations:   sim.Simulation,
				TrainingPaths: sim.TrainingPaths,
				ContractType:  sim.CallOrPut,
				ExerciseStyle: sim.ExerciseStyle,
				Seed:          deriveSeed(sim.Seed, row*columns+column),
				Workers:       sim.Workers,
			}
			result, err := PriceOption(input)
			if err != nil {
				return PricingGrids{}, fmt.Errorf("price strike %.2f at %.0f DTE: %w", strike, days, err)
			}
			results.Details[row][column] = result
			results.Price[row][column] = result.Price
			results.StandardError[row][column] = result.StandardError
			results.ExecutionTime[row][column] = result.ExecutionTime
		}
	}

	printGrid(output, "OPTION PRICE", sim, results.Price, func(value float64) string {
		return fmt.Sprintf("$%.4f", value)
	})
	printGrid(output, "STANDARD ERROR", sim, results.StandardError, func(value float64) string {
		return fmt.Sprintf("%.5f", value)
	})
	printGrid(output, "EXECUTION TIME", sim, results.ExecutionTime, func(value float64) string {
		return fmt.Sprintf("%.1fms", value)
	})
	if sim.ExerciseStyle == americanStyle {
		fmt.Fprintln(output, "\n---------------- AMERICAN DIAGNOSTICS ----------------")
		table := tabwriter.NewWriter(output, 0, 0, 3, ' ', 0)
		fmt.Fprintln(table, "Strike\tDTE\t95% interval\tAdjustment\tEarly exercise")
		for row, strike := range sim.StrikePrice {
			for col, days := range sim.ExpirationDays {
				r := results.Details[row][col]
				early := r.SimulatedPaths - r.ExerciseCounts[len(r.ExerciseCounts)-1]
				fmt.Fprintf(table, "$%.2f\t%.0fd\t[%.4f, %.4f]\t$%.4f\t%.2f%%\n", strike, days, r.ConfidenceLow, r.ConfidenceHigh, r.BoundAdjustment, 100*float64(early)/float64(r.SimulatedPaths))
			}
		}
		_ = table.Flush()
	}

	return results, nil
}

func makeGrid(rows, columns int) [][]float64 {
	grid := make([][]float64, rows)
	for row := range grid {
		grid[row] = make([]float64, columns)
	}
	return grid
}

func printGrid(output io.Writer, title string, sim *MonteCarlo, grid [][]float64, format func(float64) string) {
	fmt.Fprintf(output, "\n---------------- %s ----------------\n", title)
	writer := tabwriter.NewWriter(output, 0, 0, 2, ' ', 0)
	fmt.Fprint(writer, "Strike")
	for _, days := range sim.ExpirationDays {
		fmt.Fprintf(writer, "\t%.0fd", days)
	}
	fmt.Fprintln(writer)
	for row, strike := range sim.StrikePrice {
		fmt.Fprintf(writer, "$%.2f", strike)
		for column := range sim.ExpirationDays {
			fmt.Fprintf(writer, "\t%s", format(grid[row][column]))
		}
		fmt.Fprintln(writer)
	}
	_ = writer.Flush()
}

func effectiveTimeYears(days float64) float64 {
	if days == 0 {
		return 0.5 / 365
	}
	return days / 365
}

func stepsForDays(days float64) int {
	if days <= 0 {
		return 1
	}
	return max(1, int(math.Ceil(days*tradingDaysPerYear/365)))
}
