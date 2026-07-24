package main

import (
	"encoding/csv" // Write CSV
	"fmt"
	"math"
	"math/rand" // Randomize Variable Z
	"os"
	"runtime"
	"strconv"
	"sync"
	"time"
)

func pause() {
	fmt.Print("\n\tPress Enter to continue...")
	fmt.Scanln()
	fmt.Print("\033[H\033[2J")
}

// =================================================
// 1) Monte Carlo Option Pricing Simulation
// =================================================
func MonteCarloSim(S0, K, r, T, sigma float64, steps, simulation int, contractType string) (float64, float64) {

	dt := T / float64(steps)
	var avgPayoff float64
	var totalPayOff float64

	numWorkers := runtime.GOMAXPROCS(0) // Return Number of Usable CPU Core
	totalBatches := simulation / numWorkers
	remainder := simulation % numWorkers
	if totalBatches == 0 {
		totalBatches = 1 // In case simulation < number of CPU core
	}

	// Constant Calculation
	drift := (r - math.Pow(sigma, 2)/2.0) * dt // Drift does not change since it all constants
	volatilitySq := sigma * math.Sqrt(dt)
	start := time.Now()

	var wg sync.WaitGroup
	var mu sync.Mutex // Mutex to prevent data race on totalPayOff

	for w := 0; w < numWorkers; w++ {

		wg.Add(1) // => Create 'job'

		count := totalBatches
		if w == numWorkers-1 { // Check if it's the last worker w: 3 == 4 - 1
			count += remainder // Handle leftover, add it to the last batch for the last worker
		}
		go func(workerID int, numOfSimulation int) {

			defer wg.Done() // => Assign 'job'

			/*	Worker ID essential for the localRand so no Worker generate the same seed
				seed = time.Now().UnixNano() to ensure we seed and generate different number everytime */
			localRand := rand.New(rand.NewSource(time.Now().UnixNano() + int64(workerID)))
			var localPayoff float64

			for i := 0; i < totalBatches; i++ {
				st := S0
				for j := 0; j < steps; j++ {
					z := localRand.NormFloat64()  // Generate Rand but with bell curve distribution center 0.0
					diffusion := volatilitySq * z // Diffusion changes every Steps since Z randomize
					st = st * math.Exp(drift+diffusion)
				}
				if contractType == "CALL" {
					localPayoff += math.Max(st-K, 0)
				} else {
					localPayoff += math.Max(K-st, 0)
				}
			}
			/*	First Come First Serve (Mutex)
				Worker 1 comes first => Get to add to totalPayOff first => Key is locked
				Worker 2 waits in the meantime until Worker 1 done */
			mu.Lock()
			totalPayOff += localPayoff
			mu.Unlock()
		}(w, count)
	}
	wg.Wait()

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
func writeHeatMapCSV(sim *MonteCarlo, grid [][]float64) error {

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

// =================================================
// 6) Monte Carlo Asset Price Simulation
// =================================================
func assetPriceSim(S0, r, T, sigma float64, steps, pathCount int) [][]float64 {
	// Path Count (N): The number of parallel universes you simulate, eg: 100 different universes
	// Time Steps (M): Number of stops you make in a single path, think of it as closing period
	// eg: 252 steps as standard

	dt := T / float64(steps)
	drift := (r - math.Pow(sigma, 2)/2.0) * dt // Drift does not change since it all constants
	volatilitySq := sigma * math.Sqrt(dt)

	// Asset Price is 2D x-Price and y-Time steps
	paths := make([][]float64, pathCount)

	for i := 0; i < pathCount; i++ { // Outer loop runs once for each 'universe'

		path := make([]float64, steps+1) // Needs step+1 because Day start from 0
		path[0] = S0                     // Every 'universe' or version, start with current stock price
		st := S0
		for j := 0; j < steps; j++ { // Inner loop advances time day-by-day

			z := rand.NormFloat64()
			diffusion := volatilitySq * z
			st = st * math.Exp(drift+diffusion)
			path[j+1] = st // Save price into today's slot
		}
		paths[i] = path
	}

	return paths
}

// =================================================
// 7) Store 2D Asset Price in CSV
// =================================================
func writeAssetPriceCSV(grid [][]float64) error {

	// Instead of Store it vertical like the 3D Heatmap
	// Python's Line Plotter treats each row as a single continuous line

	fileName := "AssetPrice.csv"
	file, err := os.Create(fileName) // Create file, if exist, return err
	if err != nil {
		return fmt.Errorf("\n\tFailed to Create CSV: %v", err)
	}
	defer file.Close() // Ensure file Close

	w := csv.NewWriter(file) // Create "Writer", auto handle Escaping Special Char and auto add commas
	defer w.Flush()          // "Flush" data to the file, w/o this, file might Corrupted/Empty
	// Always place after NewWriter

	// 1. Generate a matching header for ALL columns (Step_0, Step_1, ... Step_N)
	if len(grid) > 0 {
		header := make([]string, len(grid[0]))
		for step := 0; step < len(grid[0]); step++ {
			header[step] = fmt.Sprintf("Step_%d", step)
		}
		if err := w.Write(header); err != nil {
			return err
		}
	}

	for i := 0; i < len(grid); i++ {
		row := make([]string, len(grid[i]))

		for j, price := range grid[i] {
			row[j] = strconv.FormatFloat(price, 'f', 2, 64)
		}
		if err := w.Write(row); err != nil {
			return err // Write that single data row (include 3 elements) to CSV
		}

	}

	fmt.Printf("\n\tSuccessfully Store Data in %v!\n", fileName)
	return nil
}
