package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const fmpQuoteURL = "https://financialmodelingprep.com/stable/quote"

type stockAPI struct {
	Symbol  string  `json:"symbol"`
	Price   float64 `json:"price"`
	DayLow  float64 `json:"dayLow"`
	DayHigh float64 `json:"dayHigh"`
	Volume  float64 `json:"volume"`
}

type MonteCarlo struct {
	StockSymbol      string
	UnderlyingPrice  float64
	RiskFreeRate     float64
	DividendYield    float64
	Volatility       float64
	VolatilitySource string
	StrikePrice      []float64
	ExpirationDays   []float64
	Simulation       int
	CallOrPut        string
	ExerciseStyle    string
	PathCount        int
	Volume           float64
	Seed             int64
	Workers          int
}

func (sim *MonteCarlo) fetchAPI(ctx context.Context, symbol, apiKey string) error {
	client := &http.Client{Timeout: 10 * time.Second}
	return sim.fetchAPIWithClient(ctx, symbol, apiKey, fmpQuoteURL, client)
}

func (sim *MonteCarlo) fetchAPIWithClient(ctx context.Context, symbol string, apiKey string, baseURL string, client *http.Client) error {

	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	apiKey = strings.TrimSpace(apiKey)
	if symbol == "" {
		return fmt.Errorf("ticker symbol is empty")
	}
	if apiKey == "" {
		return fmt.Errorf("FMP_API_KEY is empty")
	}
	if client == nil {
		return fmt.Errorf("HTTP client is nil")
	}

	endpoint, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("parse FMP endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("symbol", symbol)
	query.Set("apikey", apiKey)
	endpoint.RawQuery = query.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return fmt.Errorf("create FMP quote request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch quote for %s: %w", symbol, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("FMP quote request for %s failed: %s", symbol, response.Status)
	}

	var quotes []stockAPI
	if err := json.NewDecoder(response.Body).Decode(&quotes); err != nil {
		return fmt.Errorf("decode FMP quote response for %s: %w", symbol, err)
	}
	if len(quotes) == 0 {
		return fmt.Errorf("no quote data returned for symbol %s", symbol)
	}

	quote := quotes[0]
	if !isFinitePositive(quote.Price) {
		return fmt.Errorf("invalid price returned for symbol %s", symbol)
	}
	if strings.TrimSpace(quote.Symbol) == "" {
		quote.Symbol = symbol
	}

	sim.StockSymbol = strings.ToUpper(strings.TrimSpace(quote.Symbol))
	sim.Volume = quote.Volume
	sim.UnderlyingPrice = quote.Price
	sim.StrikePrice = strikePrice(quote.Price)

	volatility, volatilityErr := parkinsonVolatility(quote.DayHigh, quote.DayLow)
	if volatilityErr != nil {
		sim.Volatility = 0.25
		sim.VolatilitySource = "25% fallback; quote range was unavailable or invalid"
	} else {
		sim.Volatility = volatility
		sim.VolatilitySource = "annualized one-day Parkinson estimate"
	}
	return nil
}

func collectInput(ctx context.Context, reader *bufio.Reader, writer io.Writer, sim *MonteCarlo) error {
	apiKey := os.Getenv("FMP_API_KEY")
	if strings.TrimSpace(apiKey) == "" {
		return fmt.Errorf("set FMP_API_KEY before running the program")
	}

	symbol, err := promptRequired(reader, writer, "Ticker symbol: ")
	if err != nil {
		return err
	}
	if err := sim.fetchAPI(ctx, symbol, apiKey); err != nil {
		return err
	}
	fmt.Fprintf(writer, "\nFetched live quote for %s at $%.2f.\n", sim.StockSymbol, sim.UnderlyingPrice)

	days, err := promptFloat(reader, writer, "Target days to expiration (DTE): ", nil, func(value float64) bool {
		return isFinite(value) && value >= 0 && value == math.Trunc(value)
	})
	if err != nil {
		return err
	}
	sim.ExpirationDays = expirationDate(days)

	contract, err := promptChoice(reader, writer, "Contract type (Call/Put): ", map[string]string{
		"CALL": "CALL", "C": "CALL", "PUT": "PUT", "P": "PUT",
	})
	if err != nil {
		return err
	}
	sim.CallOrPut = contract

	style, err := promptChoice(reader, writer, "Exercise style (American/European): ", map[string]string{
		"AMERICAN": "AMERICAN", "A": "AMERICAN", "EUROPEAN": "EUROPEAN", "E": "EUROPEAN",
	})
	if err != nil {
		return err
	}
	sim.ExerciseStyle = style

	defaultDividend := sim.DividendYield * 100
	dividend, err := promptFloat(reader, writer, "Annual dividend yield (%)", &defaultDividend, func(value float64) bool {
		return isFinite(value) && value >= 0 && value <= 100
	})
	if err != nil {
		return err
	}
	sim.DividendYield = dividend / 100

	defaultRate := sim.RiskFreeRate * 100
	rate, err := promptFloat(reader, writer, "Annual risk-free rate (%)", &defaultRate, func(value float64) bool {
		return isFinite(value) && value > -100 && value <= 100
	})
	if err != nil {
		return err
	}
	sim.RiskFreeRate = rate / 100

	defaultVolatility := sim.Volatility * 100
	volatility, err := promptFloat(reader, writer, "Annual volatility (%)", &defaultVolatility, func(value float64) bool {
		return isFinite(value) && value > 0 && value <= 500
	})
	if err != nil {
		return err
	}
	if math.Abs(volatility-defaultVolatility) > 1e-12 {
		sim.VolatilitySource = "user supplied"
	}
	sim.Volatility = volatility / 100

	seed, err := promptSeed(reader, writer)
	if err != nil {
		return err
	}
	sim.Seed = seed
	if sim.Seed == 0 {
		sim.Seed = time.Now().UnixNano()
	}
	return nil
}

func promptRequired(reader *bufio.Reader, writer io.Writer, label string) (string, error) {
	for {
		fmt.Fprint(writer, label)
		value, err := readLine(reader)
		if err != nil {
			return "", err
		}
		value = strings.TrimSpace(value)
		if value != "" {
			return value, nil
		}
		fmt.Fprintln(writer, "Please enter a value.")
	}
}

func promptChoice(reader *bufio.Reader, writer io.Writer, label string, choices map[string]string) (string, error) {
	for {
		value, err := promptRequired(reader, writer, label)
		if err != nil {
			return "", err
		}
		if normalized, ok := choices[strings.ToUpper(value)]; ok {
			return normalized, nil
		}
		fmt.Fprintln(writer, "Please select one of the listed options.")
	}
}

func promptFloat(reader *bufio.Reader, writer io.Writer, label string, defaultValue *float64, valid func(float64) bool) (float64, error) {

	for {
		if defaultValue == nil {
			fmt.Fprint(writer, label)
		} else {
			fmt.Fprintf(writer, "%s [%.4g]: ", label, *defaultValue)
		}
		value, err := readLine(reader)
		if err != nil {
			return 0, err
		}
		value = strings.TrimSpace(value)
		if value == "" && defaultValue != nil && valid(*defaultValue) {
			return *defaultValue, nil
		}
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr == nil && valid(parsed) {
			return parsed, nil
		}
		fmt.Fprintln(writer, "Please enter a valid numeric value.")
	}
}

func promptSeed(reader *bufio.Reader, writer io.Writer) (int64, error) {
	for {
		fmt.Fprint(writer, "Random seed (0 creates a new seed) [0]: ")
		value, err := readLine(reader)
		if err != nil {
			return 0, err
		}
		if value == "" {
			return 0, nil
		}
		seed, parseErr := strconv.ParseInt(value, 10, 64)
		if parseErr == nil && seed >= 0 {
			return seed, nil
		}
		fmt.Fprintln(writer, "Please enter a nonnegative whole-number seed.")
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	value, err := reader.ReadString('\n')
	if err != nil && len(value) == 0 {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

func run(ctx context.Context, reader *bufio.Reader, writer io.Writer) error {
	simulation := &MonteCarlo{
		Simulation:    100_000,
		RiskFreeRate:  0.0455,
		DividendYield: 0,
		PathCount:     100,
	}
	if err := collectInput(ctx, reader, writer, simulation); err != nil {
		return err
	}

	results, err := displayOption(simulation, writer)
	if err != nil {
		return err
	}

	maxDays := simulation.ExpirationDays[len(simulation.ExpirationDays)-1]
	assetPrice, err := assetPriceSim(PricingInput{
		Spot:          simulation.UnderlyingPrice,
		Rate:          simulation.RiskFreeRate,
		DividendYield: simulation.DividendYield,
		TimeYears:     maxDays / 365,
		Volatility:    simulation.Volatility,
		Steps:         stepsForDays(maxDays),
		Simulations:   simulation.PathCount,
		Seed:          deriveSeed(simulation.Seed, 10_000),
		Workers:       simulation.Workers,
	})
	if err != nil {
		return fmt.Errorf("simulate asset paths: %w", err)
	}

	writeFiles, err := promptChoice(reader, writer, "\nStore data as CSV files (Y/N): ", map[string]string{
		"Y": "YES", "YES": "YES", "N": "NO", "NO": "NO",
	})
	if err != nil {
		return err
	}
	if writeFiles == "NO" {
		fmt.Fprintln(writer, "No files were written.")
		return nil
	}
	if err := writeHeatMapCSV("MonteCarloSim.csv", simulation, results.Price, results.StandardError); err != nil {
		return err
	}
	if err := writeAssetPriceCSV("AssetPrice.csv", assetPrice); err != nil {
		return err
	}
	fmt.Fprintln(writer, "Saved MonteCarloSim.csv and AssetPrice.csv.")
	return nil
}

func main() {
	reader := bufio.NewReader(os.Stdin)
	if err := run(context.Background(), reader, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func isFinitePositive(value float64) bool {
	return isFinite(value) && value > 0
}
