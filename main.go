package main

import (
	"encoding/json" // Read API Json
	"fmt"
	"net/http" // Take URL
	"os"
	"strconv" // Used for input validation
	"strings"
	"time"
)

// =================================================
// 1) Stock API Object
// =================================================
type stockAPI struct {
	Symbol  string  `json:"symbol"`
	Price   float64 `json:"price"`
	DayLow  float64 `json:"dayLow"`
	DayHigh float64 `json:"dayHigh"`
	Volume  float64 `json:"volume"`
}

// =================================================
// 1.1) Fetch API Method
// =================================================
func (sim *MonteCarlo) fetchAPI(symbol string, apiKey string) error {

	if apiKey == "" {
		return fmt.Errorf("\n\tError: FMP_API_KEY is empty!\n")
	}

	url := fmt.Sprintf("https://financialmodelingprep.com/stable/quote?symbol=%s&apikey=%s", symbol, apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)

	if err != nil {
		return fmt.Errorf("\n\tError: Failed to make HTTP request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("\n\tError: Unable to Fetch the Selected Symbol (%v)", symbol)
	}

	var api []stockAPI
	if err := json.NewDecoder(response.Body).Decode(&api); err != nil {
		return fmt.Errorf("\n\tError: Failed to decode JSON: %v", err)
	}

	if len(api) == 0 {
		return fmt.Errorf("\n\tNo data return for symbol: %v", err)
	}

	data := api[0]
	sim.StockSymbol = data.Symbol
	sim.UnderlyingPrice = data.Price
	sim.StrikePrice = strikePrice(data.Price)
	sim.Volatility = parkinsonVolatility(data.DayHigh, data.DayLow)
	return nil
}

// =================================================
// 1.2) Store API Function
// =================================================
func storeAPI(sim *MonteCarlo) {

	var symbol string

	apiKey := os.Getenv("FMP_API_KEY") // fallback key instead of hard-code key
	fmt.Print("\n\tPlease enter a ticker symbol: ")
	fmt.Scanln(&symbol)

	for {
		var day string // To Validate, Must Start With String
		fmt.Print("\tTarget days to expiration (DTE): ")
		fmt.Scanln(&day)
		day = strings.TrimSpace(day)               // Clean Up the Input String
		expDay, err := strconv.ParseFloat(day, 64) // Parse string 'day' into float64 'expDay' to pass to a func
		if err != nil {
			fmt.Print("\n\tError: Expecting Positive Number.\n")
		} else if expDay < 0 {
			fmt.Print("\n\tError: Expecting Positive Value.\n")
		} else {
			sim.ExpirationDays = expirationdDate(expDay)
			break
		}
	}

	for {
		var contract string
		fmt.Print("\tSelect type of Contract (Call/Put): ")
		fmt.Scan(&contract)
		contract = strings.TrimSpace(strings.ToUpper(contract)) // Clean + Capitalize Strings
		_, err := strconv.ParseFloat(contract, 64)              // Parse to Float, if input is indeed Float
		if err == nil {                                         // Which is err == nil, then print Error
			fmt.Print("\n\tError: Expecting Type of Contract (Call/Put)\n")
		} else if contract != "CALL" && contract != "PUT" {
			fmt.Print("\n\tError: Please Select The Type of Contract.\n")
		} else {
			sim.CallOrPut = contract
			break
		}
	}
	err := sim.fetchAPI(strings.ToUpper(symbol), apiKey)
	if err != nil {
		fmt.Print(err)
		pause()
		os.Exit(0)
	} else {
		fmt.Printf("\n\tSuccessfully Fetch Live Data for Stock Symbol: %v\n", strings.ToUpper(symbol))
		pause()
	}
}

// =================================================
// 2) Monte Carlo Simulation Object
// =================================================
type MonteCarlo struct {
	StockSymbol     string
	UnderlyingPrice float64   // S0
	RiskFreeRate    float64   // r
	Volatility      float64   // sigma
	StrikePrice     []float64 // K
	ExpirationDays  []float64 // T in days
	Steps           int       // 252 for Standard, 100 for shorter interval
	Simulation      int       // 50000 to 100000, 100000 for Standard
	CallOrPut       string    // Call or Put Contract
}

type Performance struct {
	ExecutionTime [][]int // Execution Time (ms)
}

func main() {

	simulation := &MonteCarlo{}
	simulation.Steps = 252
	simulation.Simulation = 100000
	simulation.RiskFreeRate = 0.0455
	storeAPI(simulation)

	numRows := len(simulation.StrikePrice)
	numCols := len(simulation.ExpirationDays)
	// Initialize Grid
	grid := make([][]float64, numRows)
	timeGrid := make([][]float64, numRows)
	for i := 0; i < numRows; i++ {
		grid[i] = make([]float64, numCols)
		timeGrid[i] = make([]float64, numCols)
	}

	displayOption(simulation, grid, timeGrid)

	isWrite := false
	for {
		var writeFile string
		fmt.Print("\n\t- Store Data as CSV File (Y/N): ")
		fmt.Scanln(&writeFile)
		writeFile = strings.TrimSpace(strings.ToUpper(writeFile)) // Clean + Capitalize Strings
		_, err := strconv.ParseFloat(writeFile, 64)               // Parse to Float, if input is indeed Float
		if err == nil {                                           // Which is err == nil, then print Error
			fmt.Print("\n\tExpecting Yes or No.\n")
		} else if writeFile != "Y" && writeFile != "YES" && writeFile != "N" && writeFile != "NO" {
			fmt.Print("\n\tPlease Select The Correct Option.\n")
		} else if writeFile != "Y" && writeFile != "YES" {
			fmt.Print("\n\tProgram Terminated!\n")
			pause()
			os.Exit(0)
		} else {
			isWrite = true
			writeCSV(simulation, grid)
		}
		if isWrite {
			break
		}
	}
	pause()

}

// Several things add:
// Path Count
// Execution Time (ms) // Done
// Standard Error
// Greeks
// Write file to Create Heat Map // Done
