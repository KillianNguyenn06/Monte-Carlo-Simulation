package main

import (
	"fmt"
	"io"
	"math"
	"text/tabwriter"
)

type PricingGrids struct {
	Price         [][]float64
	StandardError [][]float64
	ExecutionTime [][]float64
}

func displayOption(sim *MonteCarlo, output io.Writer) (PricingGrids, error) {
	rows := len(sim.StrikePrice)
	columns := len(sim.ExpirationDays)
	results := PricingGrids{
		Price:         makeGrid(rows, columns),
		StandardError: makeGrid(rows, columns),
		ExecutionTime: makeGrid(rows, columns),
	}

	fmt.Fprintln(output, "\n================ OPTION PRICE SIMULATION ================")
	fmt.Fprintf(output, "Symbol: %s\tVolume: %s\n", sim.StockSymbol, formatNumber(sim.Volume))
	fmt.Fprintf(output, "Contract: %s\tExercise: %s\n", sim.CallOrPut, sim.ExerciseStyle)
	fmt.Fprintf(output, "Spot: $%.2f\tRate: %.3f%%\tDividend: %.3f%%\tVolatility: %.3f%%\n",
		sim.UnderlyingPrice,
		sim.RiskFreeRate*100,
		sim.DividendYield*100,
		sim.Volatility*100,
	)
	fmt.Fprintf(output, "Volatility source: %s\n", sim.VolatilitySource)
	fmt.Fprintf(output, "Paths per contract: %d\tSeed: %d\n", sim.Simulation, sim.Seed)

	for row, strike := range sim.StrikePrice {
		for column, days := range sim.ExpirationDays {
			input := PricingInput{
				Spot:          sim.UnderlyingPrice,
				Strike:        strike,
				Rate:          sim.RiskFreeRate,
				DividendYield: sim.DividendYield,
				TimeYears:     effectiveTimeYears(days),
				Volatility:    sim.Volatility,
				Steps:         stepsForDays(days),
				Simulations:   sim.Simulation,
				ContractType:  sim.CallOrPut,
				ExerciseStyle: sim.ExerciseStyle,
				Seed:          deriveSeed(sim.Seed, row*columns+column),
				Workers:       sim.Workers,
			}
			result, err := PriceOption(input)
			if err != nil {
				return PricingGrids{}, fmt.Errorf("price strike %.2f at %.0f DTE: %w", strike, days, err)
			}
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
