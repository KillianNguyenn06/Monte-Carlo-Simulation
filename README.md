# Monte Carlo Option Pricing Simulator

A Go and Python project for estimating American- and European-style option values with Monte Carlo methods. The CLI fetches a live underlying quote from Financial Modeling Prep (FMP), prices a strike/expiration grid, reports simulation uncertainty, exports CSV data, and produces interactive Plotly visualizations.

## Pricing models

- **American style:** Longstaff–Schwartz least-squares Monte Carlo (LSM). Full asset paths are simulated and processed backward to compare immediate exercise value with estimated continuation value.
- **European style:** terminal-payoff Monte Carlo. Each path is valued only at expiration.

Exercise style describes contract rights, not geography. Standard U.S. equity and ETF options are generally American-style, while some U.S. index options are European-style. Select the style that matches the actual contract.

## Features

- American call and put pricing with early-exercise decisions
- European call and put pricing
- Dividend yield in risk-neutral asset drift
- Deterministic runs with a user-supplied random seed
- Bounded parallel simulation with exact path counts
- Five strikes from 90% to 110% of spot
- Five expirations at seven-day intervals
- Price, Monte Carlo standard-error, and execution-time grids
- CSV exports for option prices and simulated asset paths
- Correctly oriented interactive 3D price surface
- Interactive simulated asset-price chart
- Input, HTTP response, model, and CSV validation

## Model outline

Asset paths use geometric Brownian motion under the risk-neutral measure:

```text
S(t + dt) = S(t) * exp((r - q - sigma²/2)dt + sigma*sqrt(dt)*Z)
```

where `r` is the risk-free rate, `q` is dividend yield, `sigma` is annualized volatility, and `Z` is a standard normal random value.

The American pricer regresses discounted future cash flows on `1`, normalized spot, and normalized spot squared at each exercise step. An in-the-money path exercises when intrinsic value exceeds estimated continuation value.

## Prerequisites

- Go 1.22 or newer
- Python 3.9 or newer
- An FMP API key

## Quick start

```bash
git clone git@github.com:KillianNguyenn06/Monte-Carlo-Simulation.git
cd Monte-Carlo-Simulation
```

Create the project-local Python environment:

```bash
make setup
```

The equivalent manual commands are:

```bash
python3 -m venv .venv
.venv/bin/python -m pip install --upgrade pip
.venv/bin/python -m pip install -r requirements.txt
```

On Windows PowerShell:

```powershell
.venv\Scripts\Activate.ps1
python -m pip install --upgrade pip
python -m pip install -r requirements.txt
```

Set the FMP API key:

```bash
export FMP_API_KEY="your_api_key"
```

On Windows PowerShell:

```powershell
$env:FMP_API_KEY = "your_api_key"
```

Run the CLI:

```bash
make run
```

The program requests:

1. Underlying ticker
2. Starting days to expiration
3. Call or put
4. American or European exercise style
5. Annual dividend yield
6. Annual risk-free rate
7. Annual volatility
8. Random seed
9. Whether to export CSV files

Press Enter at a bracketed prompt to accept its displayed default. Seed `0` creates a new seed; enter the printed seed again to reproduce a run.

The quote's daily high and low produce a one-day annualized Parkinson volatility estimate. The prompt allows that estimate to be replaced. If the quote range is unavailable or invalid, the displayed default is a clearly labeled 25% fallback.

To run the Go application and then open both Python visualizations with one command:

```bash
make run-all
```

Choose `Y` when the Go application asks whether to export CSV files. For HTML generation without opening browser windows:

```bash
make run-all-headless
```

## Model inputs

- **Exercise style:** American uses Longstaff–Schwartz early-exercise decisions; European uses terminal payoff.
- **Dividend yield:** enters risk-neutral drift as `r - q` and is especially important for American calls.
- **Risk-free rate:** should reflect a maturity reasonably close to the option's expiration.
- **Volatility:** defaults to the quote's high/low Parkinson estimate but can be overridden for scenario analysis.
- **Random seed:** zero creates a new time-based seed; a fixed nonzero value reproduces the same paths.

## Outputs

Selecting CSV export creates:

- `MonteCarloSim.csv`: strike, expiration, estimated option price, standard error, and model metadata
- `AssetPrice.csv`: one simulated asset path per row

Generate both charts from existing CSV files:

```bash
make plots
```

Use `make plots-headless` to create HTML without opening a browser. Custom paths are also supported:

```bash
.venv/bin/python plot_3d.py --input MonteCarloSim.csv --output option_surface.html
.venv/bin/python plot_path.py --input AssetPrice.csv --output asset_paths.html
```

The asset-path chart spans the largest generated expiration: the selected DTE plus 28 calendar days. The program converts that horizon to approximately one point per U.S. trading day using `ceil(maxDTE × 252 / 365)`. For example, a starting DTE of 7 produces a 35-day maximum horizon and approximately 25 trading steps.

## Defaults

| Setting | Default |
|---|---:|
| Paths per option contract | 100,000 |
| Trading/exercise steps | 252 per year, scaled to each DTE |
| Displayed asset paths | 100 |
| Risk-free rate | 4.55% |
| Dividend yield | 0% |
| Strike grid | 90%, 95%, 100%, 105%, 110% of spot |
| Expiration grid | Selected DTE plus 0, 7, 14, 21, 28 days |

## Test

```bash
make test
```

Tests cover deterministic simulation, worker-count independence, Parkinson volatility, a Black–Scholes European benchmark, American lower bounds, regression, API responses, CSV output, and non-square plot orientation.

## Project structure

```text
.
├── main.go          # CLI, FMP quote retrieval, and workflow
├── function.go      # Pricing engines, path simulation, statistics, and CSV output
├── display.go       # Price, standard-error, and execution-time tables
├── plot_3d.py       # Interactive option-price surface
├── plot_path.py     # Interactive simulated asset paths
├── function_test.go # Numerical, deterministic, and CSV tests
├── main_test.go     # FMP response tests
├── test_plots.py    # Visualization data tests
├── requirements.txt # Direct Python dependencies
├── go.mod           # Go module and minimum Go version
├── Makefile         # Setup, run, plot, and test commands
└── README.md         # Project documentation
```

## Important limitations

- This is an educational estimator, not financial advice or a trading system.
- The application fetches an underlying quote, not a listed option chain. Strikes and expirations are generated rather than retrieved from an exchange listing.
- The default volatility is based on one daily high/low range, not contract implied volatility or a volatility surface.
- Dividend yield is continuous; discrete ex-dividend dates are not modeled.
- One risk-free rate is used for the full grid rather than a maturity-specific yield curve.
- Longstaff–Schwartz is a statistical lower-bound estimator and remains sensitive to path count, exercise steps, and regression basis.
- Trading calendars, settlement rules, bid/ask spreads, transaction costs, taxes, and contract-specific multipliers are not modeled.

For market use, obtain actual option-chain metadata and compare the model estimate with the corresponding contract's bid, ask, and implied volatility.

## License

No license is currently included. Add one before distributing the project or accepting external contributions.
