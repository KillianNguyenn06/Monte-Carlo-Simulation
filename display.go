package main

import (
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
)

// =================================================
// 1) Display Option's Contract Pricing
// =================================================
func displayOption(sim *MonteCarlo, data *stockAPI, grid [][]float64, timeGrid [][]float64) ([][]float64, [][]float64) {

	// Header Info (Indented)
	fmt.Println("\n\t====================== OPTION'S CONTRACT PRICE ======================")
	fmt.Printf("\n\tStock Symbol : %v", sim.StockSymbol)
	fmt.Printf("\t\t\t\t\tVolume: %v", formatNumber(sim.Volume))
	fmt.Printf("\n\tContract Type: %v", sim.CallOrPut)
	fmt.Printf("\n\tS0 = $%.2f, r = %.1f%%, σ = %.1f%%\n\n", sim.UnderlyingPrice, sim.RiskFreeRate*100, sim.Volatility*100)

	fmt.Printf("\t%-*s", 22, "Strike Prices (K):")
	for _, strikes := range sim.StrikePrice {
		fmt.Printf("%-*.2f", 10, strikes)
	}

	fmt.Println()
	fmt.Printf("\t%-*s", 22, "Expiration (days):")
	for _, days := range sim.ExpirationDays {
		dayString := fmt.Sprintf("%.0fd", days)
		fmt.Printf("%-*v", 10, dayString)
	}
	fmt.Println("\n\t____________________________________________________________________")

	var wg sync.WaitGroup

	for i, strikes := range sim.StrikePrice {
		for j, days := range sim.ExpirationDays {
			wg.Add(1)                                    // Create 1 Goroutine "job"
			go func(row, col int, strike, exp float64) { // Go goroutine
				defer wg.Done() // Delete that 1 "job" when someone already take it

				var daysInYear float64
				if exp == 0 {
					daysInYear = 0.5 / 365.0 // Handle 0 DTE
				} else {
					daysInYear = exp / 365.0
				}
				grid[row][col], timeGrid[row][col] = MonteCarloSim(sim.UnderlyingPrice, strike, sim.RiskFreeRate, daysInYear, sim.Volatility, sim.Steps, sim.Simulation, sim.CallOrPut)

			}(i, j, strikes, days)
		}
	}
	wg.Wait() // Wait for all Go goroutine to finish before move on

	// Initialize tabwriter
	w := tabwriter.NewWriter(os.Stdout, 7, 0, 2, ' ', 0)

	// 1. Table Header Row (Prefixed with \t)
	fmt.Fprint(w, "\t\t")
	for _, days := range sim.ExpirationDays {
		fmt.Fprintf(w, "%8s\t", fmt.Sprintf("%.0fd", days))
	}
	fmt.Fprintln(w)

	// 2. Table Separator Row (Prefixed with \t)
	fmt.Fprint(w, "\t____________\t")
	for range sim.ExpirationDays {
		fmt.Fprint(w, "___________")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)

	fmt.Println()
	// 3. Matrix Data Rows (Prefixed with \t)
	for i, K := range sim.StrikePrice {
		fmt.Fprintf(w, "\t%8s\t", fmt.Sprintf("$%.2f", K))
		fmt.Fprintf(w, "\t")
		for j := range sim.ExpirationDays {
			fmt.Fprintf(w, "%-8s\t", fmt.Sprintf("$%.2f", grid[i][j]))
		}
		fmt.Fprintln(w)
		fmt.Fprint(w, "\t\t\t")
		for range sim.ExpirationDays {
			fmt.Fprint(w, "\t")
		}
		fmt.Fprintln(w)
	}

	// Render table
	w.Flush()
	fmt.Println("\t=====================================================================")

	performance(sim, timeGrid)
	fmt.Print("\n\tNote: ")
	fmt.Print("\n\t- Prices decrease as strike increases")
	fmt.Print("\n\t- Prices increase as expiration increases\n")

	return grid, timeGrid
}

// =================================================
// 2) Display Simulation's Performance
// =================================================
func performance(sim *MonteCarlo, t [][]float64) {
	// Title printed outside tabwriter so it doesn't stretch columns
	fmt.Println("\n\t\t   -------------- PERFORMANCE --------------")

	// Initialize tabwriter
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)

	// Table Header Row (First \t is empty space for the Strike Price column)
	fmt.Fprint(w, "\t\t")
	for _, days := range sim.ExpirationDays {
		fmt.Fprintf(w, "%s\t", fmt.Sprintf("%.0fd", days))
	}
	fmt.Fprintln(w)

	// Table Separator Row
	fmt.Fprint(w, "\t____________\t")
	for range sim.ExpirationDays {
		fmt.Fprint(w, "___________\t")
	}
	fmt.Fprintln(w)

	// Matrix Data Rows
	for i, K := range sim.StrikePrice {
		// Column 1: Strike Price ($489.99)
		fmt.Fprintf(w, "\t%s\t", fmt.Sprintf("$%.2f", K))

		// Column 2..N: Expiration Times (5020.9ms, etc.)
		for j := range sim.ExpirationDays {
			fmt.Fprintf(w, "%s\t", fmt.Sprintf("%.1fms", t[i][j]))
		}
		fmt.Fprintln(w)
	}

	// Render table
	w.Flush()
	fmt.Println("\t=====================================================================")
}
