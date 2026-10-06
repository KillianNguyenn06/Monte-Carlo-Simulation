# Monte Carlo Option Pricing Simulator

A Go and Python project for estimating American- and European-style option values with Monte Carlo methods. The CLI fetches a live underlying quote from Financial Modeling Prep (FMP), prices a strike/expiration grid, reports simulation uncertainty, exports CSV data, and produces interactive Plotly visualizations.

## Pricing models

- **American style:** Longstaff–Schwartz least-squares Monte Carlo (LSM). A separate training simulation learns exercise decisions backward. A fresh valuation simulation then applies the frozen policy forward.
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

The American pricer regresses discounted future cash flows on a quadratic basis at each exercise step. Inputs are centered and scaled; a two-pass modified Gram–Schmidt QR solve avoids normal equations. Coefficients remain in that standardized basis. A rank-deficient fit uses a reported constant-mean fallback; a step without in-the-money training paths continues without a fitted exercise rule.

Training defaults to 50,000 paths **in addition to** 100,000 independent valuation paths. Training and valuation use separate derived seeds. Each strike/expiration still generates its own paths: no sharing across strikes, antithetic sampling, control variates, RNG optimization, or buffer reuse has been introduced.

The time-zero exercise decision is made using training data only. Valuation applies the frozen rule to current stock prices, without using a path's future to decide whether to exercise. The main price is this raw policy estimate. Its standard error is `sample SD(discounted valuation payoffs) / sqrt(valuation paths)`, with an approximate 95% interval `price ± 1.959964 × SE`. This interval is conditional on the trained policy: it excludes training variability, regression bias, exercise-grid error and model/input error. A zero SE can mean a trained policy always exercises immediately; it does not prove the policy is optimal.

For diagnostics, `BoundAdjustedPrice = max(raw price, same-valuation-path European estimate, intrinsic value)` and `BoundAdjustment` are reported separately. This sample-based adjustment is not a guaranteed bound on the exact price and does not overwrite the raw estimate or its SE/CI. An admissible fixed policy has value at most the optimal American value in expectation; any finite Monte Carlo estimate can lie above or below it.

## Prerequisites

- Go 1.22 or newer
- Python 3.9 or newer
- An FMP API key
- A FRED API key for automatic Treasury rates (manual input remains available)

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
export FRED_API_KEY="your_fred_api_key"
```

On Windows PowerShell:

```powershell
$env:FMP_API_KEY = "your_api_key"
$env:FRED_API_KEY = "your_fred_api_key"
```

Keys are read from process environment variables. `.env` and `.env.*` files are ignored by Git, but the CLI does not automatically load them; export the variables in your shell or use your preferred environment loader. Do not place keys in source code.

Run the CLI:

```bash
make run
```

The program requests:

1. Underlying ticker
2. Whole-number days to expiration (DTE); only `0` asks for today’s expiration time
3. Call or put
4. American or European exercise style
5. Additional training path count for American options (default 50,000)
6. Annual dividend yield
7. FRED or manual rates; review dated automatic rates or enter an override
8. Annual volatility
9. Random seed
10. Whether to export CSV files

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
- **Risk-free rate:** FRED-derived maturity-specific continuously compounded proxies, or an explicit manual continuous annual rate. The static 4.55% default is removed.
- **Volatility:** defaults to the quote's high/low Parkinson estimate but can be overridden for scenario analysis.
- **Random seed:** zero creates a new time-based seed; a fixed nonzero value reproduces the same paths.

## Expiration timing

The CLI asks for whole-number DTE first:

- **Positive DTE:** no time or date prompt. `7` means seven full 24-hour days, and the grid remains `7, 14, 21, 28, 35` days. This is a simple day-count scenario, not an exact listed-contract expiration timestamp.
- **0 DTE:** asks for today's expiration time as `HH:MM` in **America/New_York**, such as `16:00`. An RFC3339 timestamp with explicit offset is also accepted, but must fall on the same New York date as the valuation snapshot. Past times are rejected. Ambiguous daylight-saving times require an explicit offset; nonexistent times are rejected.

The valuation clock is frozen immediately after receiving the underlying quote. Answering prompts does not advance the snapshot. This is quote receipt time, not a verified exchange quote timestamp; stale underlying quotes remain a limitation. For 0 DTE, actual remaining seconds determine the first horizon. The four later synthetic expirations preserve that New York wall time at seven-calendar-day intervals, accounting for daylight-saving changes.

Time to expiry uses ACT/365F. At exactly `TimeYears=0`, the engine returns intrinsic value with zero sampling error and no training or simulation. Negative engine horizons are rejected. Historical settlement requires the applicable settlement price, not today's quote.

Console horizons show hours below one day and fractional days otherwise. CSVs preserve full fractional `Expiration_Y` values and the valuation timestamp. `ExpirationTimestamp` is populated for the exact-time 0-DTE workflow and left blank for positive-DTE scenarios. Charts use the same horizon. Contract calendars and settlement conventions remain outside this implementation, and the exercise grid still uses approximately 252 steps per year.

## FRED rates

The CLI requests the needed bracketing Treasury maturities from FRED once per run, not per strike. Supported series are `DGS1MO`, `DGS3MO`, `DGS6MO`, `DGS1`, `DGS2`, `DGS3`, `DGS5`, `DGS7`, `DGS10`, `DGS20`, and `DGS30`. It selects the latest common valid observation date across the requested series, skips missing observations (`.`), and rejects future dates and observations older than seven calendar days.

This is a **par-as-zero approximation**, not a bootstrapped zero-coupon curve. For a quoted annual decimal yield `y` at tenor `t` years, the proxy is:

- At tenors up to six months: `r = log(1 + y*t) / t` (simple ACT/365 approximation).
- At longer tenors: `r = 2*log(1 + y/2)` (semiannual compounding approximation).

Continuous rates are linearly interpolated between maturities. Below one month the one-month proxy is held flat; maturities above 30 years require manual input. Each contract uses its selected rate as a constant throughout its drift and discounting, including intermediate exercise dates. A full time-dependent discount curve is a later extension. The displayed asset paths use the rate for the longest generated expiration.

The source, date and convention are saved in the option CSV. Public observations are cached at `.cache/fred-rates.json` (ignored by Git). If the live fetch fails, a complete cache no older than seven days may be offered, visibly labeled as cached and requiring acceptance. Otherwise a manual rate is required; no silent default is substituted. A manual override applies the same continuous rate across the grid. Preserve the printed/exported rates to reproduce a run later, since automatic inputs change over time.

References: [FRED observations API](https://fred.stlouisfed.org/docs/api/fred/series_observations.html), [Treasury yield curve methodology](https://home.treasury.gov/policy-issues/financing-the-government/interest-rate-statistics/treasury-yield-curve-methodology).

## Outputs

Selecting CSV export creates:

- `MonteCarloSim.csv`: strike, expiration, raw estimated option price, standard error, rate provenance, training/valuation counts and seeds, conditional confidence interval, separate bound diagnostics and regression/exercise diagnostics. `ExerciseCounts` lists counts from step zero to maturity separated by semicolons; the final bucket includes worthless expirations. American-only floating diagnostics are blank for European contracts.
- `AssetPrice.csv`: one simulated asset path per row, with calendar-day coordinates in column headers

Generate both charts from existing CSV files:

```bash
make plots
```

Use `make plots-headless` to create HTML without opening a browser. Custom paths are also supported:

```bash
.venv/bin/python plot_3d.py --input MonteCarloSim.csv --output option_surface.html
.venv/bin/python plot_path.py --input AssetPrice.csv --output asset_paths.html
```

The asset-path chart uses calendar days on its horizontal axis and spans the largest generated expiration: the selected DTE plus 28 days for positive-DTE scenarios. For 0 DTE, it ends at the fifth generated expiration, preserving the entered New York wall time across daylight-saving changes. The program converts that horizon to approximately one point per U.S. trading day using `ceil(maxDTE × 252 / 365)`. For example, a starting DTE of 7 produces a 35-day maximum horizon and approximately 25 trading steps. Those 25 steps span the full 35 calendar days; the chart now ends at day 35. New asset CSVs encode the calendar coordinates as `Day_0`, ..., `Day_35`. Legacy `Step_` CSVs remain readable and show simulation steps unless their known actual horizon is supplied with `--horizon-days 35`. This option relabels existing data; it does not extend or resimulate paths. A 252-trading-step, one-year horizon would require a separate simulation and is not enabled by this display change. Hover now shows the nearest path rather than all 100 paths at once.

The console summary omits repeated rate rows; rates remain visible during input and in the option CSV. American diagnostics use an aligned table with the 95% interval, bound adjustment, and early-exercise percentage. Full regression and exercise-count diagnostics remain in CSV; statistical interpretation is documented above.

## Defaults

| Setting | Default |
|---|---:|
| Valuation paths per option contract | 100,000 |
| Additional American training paths | 50,000 (configurable) |
| Trading/exercise steps | 252 per year, scaled to each DTE |
| Displayed asset paths | 100 |
| Risk-free rate | FRED maturity proxy, with explicit manual fallback |
| Dividend yield | 0% |
| Strike grid | 90%, 95%, 100%, 105%, 110% of spot |
| Expiration grid | Positive DTE + 0, 7, 14, 21, 28 days; 0 DTE uses today’s time plus weekly dates |

## Test

```bash
make test
```

Tests cover deterministic simulation, worker-count independence, independent training/valuation streams, frozen-policy decisions, clustered and rank-deficient regression, multi-seed American put/dividend-call/short-low-volatility benchmarks, Black–Scholes, FRED missing/stale/future observations, cache fallback, rate conversion/interpolation, manual overrides, API responses, CSV diagnostics, expiration timestamps, DST boundaries, exact-expiry payoffs, fractional CSV horizons, and plotting. HTTP tests use local servers, not live credentials. Numerical tolerances include separately stated sampling and exercise/regression allowances; a pass is not a claim of market calibration.

## Project structure

```text
.
├── main.go          # CLI, FMP quote retrieval, and workflow
├── function.go      # Pricing engines, path simulation, statistics, and CSV output
├── american.go      # Independent LSM training and valuation
├── regression.go    # Centered/scaled QR continuation regression
├── rates.go         # FRED retrieval, cache, conventions and rate prompts
├── american_test.go # Independent valuation and regression tests
├── rates_test.go    # Rate provider and CLI tests
├── expiration.go   # Explicit timestamps, New York time and fractional horizons
├── expiration_test.go # Expiry, DST and timestamp-export checks
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
- Automatic Treasury rates are maturity-specific par-as-zero proxies; they do not constitute a fully calibrated discount curve.
- The trained exercise policy remains sensitive to training size, exercise steps and regression basis. The displayed CI measures valuation sampling uncertainty only.
- Positive DTE uses whole-day scenarios; only 0 DTE uses an exact expiration time. Listed-contract calendars and settlement conventions are not fetched or inferred.
- Independent training adds computation and memory use; computational-efficiency changes are deferred.
- Trading calendars, settlement rules, bid/ask spreads, transaction costs, taxes, and contract-specific multipliers are not modeled.

For market use, obtain actual option-chain metadata and compare the model estimate with the corresponding contract's bid, ask, and implied volatility.

## License

No license is currently included. Add one before distributing the project or accepting external contributions.
