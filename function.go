package main

import (
	"encoding/csv" // Write CSV
	"fmt"
	"math"
	"math/rand" // Randomize Variable Z
	"os"
	"time"
)

func pause() {
	fmt.Print("\n\tPress Enter to continue...")
	fmt.Scanln()
	fmt.Print("\033[H\033[2J")
}

// =================================================
// 1) Monte Carlo Simulation Calculation
// =================================================
func MonteCarloSim(S0, K, r, T, sigma float64, steps, simulation int, contractType string) (float64, float64) {

	dt := T / float64(steps)
	var avgPayoff float64
	var totalPayOff float64

	drift := (r - math.Pow(sigma, 2)/2.0) * dt // Drift does not change since it all constants
	start := time.Now()

	for i := 0; i < simulation; i++ {
		st := S0
		for j := 0; j < steps; j++ {
			z := rand.NormFloat64()                // Generate Rand but with bell curve distribution center 0.0
			diffusion := sigma * math.Sqrt(dt) * z // Diffusion changes every Steps since Z randomize
			st = st * math.Exp(drift+diffusion)
		}
		var payoff float64
		if contractType == "CALL" {
			payoff = math.Max(st-K, 0)
		} else {
			payoff = math.Max(K-st, 0)
		}
		totalPayOff += payoff
	}

	elapsed := float64(time.Since(start).Microseconds()) / 1000.0
	avgPayoff = totalPayOff / float64(simulation)
	discountedPrice := math.Exp(-r*T) * avgPayoff

	return discountedPrice, float64(elapsed)
}

// =================================================
// 2) Generate Expiration Date:
// 0dte, +7dte, +7dte, +30dte, +30dte
// =================================================
func expirationdDate(exp float64) []float64 {

	current := exp
	expiration := make([]float64, 5)
	expiration[0] = current

	for i := 1; i <= 4; i++ {
		current += 7
		expiration[i] += current
	}

	return expiration
}

// =================================================
// 3) Generate Strike Price based on Current Price
// 90%, 95%, 1, 105%, 110%
// =================================================
func strikePrice(s float64) []float64 {
	strike := []float64{s * 0.90, s * 0.95, s, s * 1.05, s * 1.10}
	return strike
}

// =================================================
// 4) Generate Volatility based on Parkinson Sim
// =================================================
func parkinsonVolatility(high, low float64) float64 {
	if low < 0 || high <= low {
		return 0.25
	}

	logHL := math.Log(high / low)
	volatility := math.Sqrt(math.Pow(logHL, 2)/4*math.Log(2)) * math.Sqrt(252)

	return volatility
}

// =================================================
// 5) Store Strikes, Expiration, OptionPrice in CSV
// =================================================
func writeCSV(sim *MonteCarlo, grid [][]float64) error {

	fileName := "MonteCarloSim.csv"
	file, err := os.Create(fileName) // Create file, if exist, return err
	if err != nil {
		return fmt.Errorf("\n\tFailed to Create CSV: %v", err)
	}
	defer file.Close() // Ensure file Close

	w := csv.NewWriter(file) // Create "Writer", auto handle Escaping Special Char and auto add commas
	defer w.Flush()          // "Flush" data to the file, w/o this, file might Corrupted/Empty
	// Always place after NewWriter

	header := []string{"Strike_X", "Expiration_Y", "OptionPrice_Z"} // Define Column's Name
	if err := w.Write(header); err != nil {
		return err
	}

	for i, strike := range sim.StrikePrice {
		for j, days := range sim.ExpirationDays {
			row := []string{ // Build a 3-element string slice representing 1 single coordinate row in CSV
				fmt.Sprintf("%.2f", strike),
				fmt.Sprintf("%.0f", days),
				fmt.Sprintf("%.2f", grid[i][j]),
			}
			if err := w.Write(row); err != nil {
				return err // Write that single data row (include 3 elements) to CSV
			}
		}
	}
	fmt.Printf("\n\tSuccessfully Store Data in %v!\n", fileName)
	return nil
}
