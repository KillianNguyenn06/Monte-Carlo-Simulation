package main

import (
	"math"
	"reflect"
	"testing"
)

func TestRegressionClusteredAndRankDeficient(t *testing.T) {
	x := []float64{1 - 2e-8, 1 - 1e-8, 1, 1 + 1e-8, 1 + 2e-8}
	y := make([]float64, len(x))
	for i, v := range x {
		z := (v - 1) / 1e-8
		y[i] = 2 + 3*z + 4*z*z
	}
	m, err := fitContinuation(x, y)
	if err != nil || m.Fallback {
		t.Fatalf("clustered fit failed: %+v %v", m, err)
	}
	for i, v := range x {
		if math.Abs(m.predict(v)-y[i]) > 1e-8 {
			t.Fatalf("prediction %d: %.12f vs %.12f", i, m.predict(v), y[i])
		}
	}
	m, err = fitContinuation([]float64{1, 1, 1}, []float64{2, 4, 6})
	if err != nil || !m.Fallback || m.predict(1) != 4 {
		t.Fatalf("constant fallback: %+v %v", m, err)
	}
	if _, err = fitContinuation([]float64{1, 2, math.NaN()}, []float64{1, 2, 3}); err == nil {
		t.Fatal("accepted NaN")
	}
}

func TestAmericanSeparateStreamsAndWorkerDeterminism(t *testing.T) {
	p := testPricingInput()
	p.ExerciseStyle = americanStyle
	p.ContractType = putContract
	p.Simulations = 3000
	p.TrainingPaths = 4000
	p.Steps = 20
	p.Workers = 1
	a, err := PriceOption(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Workers = 4
	b, err := PriceOption(p)
	if err != nil {
		t.Fatal(err)
	}
	a.ExecutionTime, b.ExecutionTime = 0, 0
	if !reflect.DeepEqual(a, b) {
		t.Fatal("worker count changed valuation")
	}
	if a.TrainingSeed == a.ValuationSeed || a.SimulatedPaths != 3000 || a.TrainingPaths != 4000 {
		t.Fatalf("wrong streams or counts %+v", a)
	}
	n := 0
	for _, count := range a.ExerciseCounts {
		n += count
	}
	if n != 3000 {
		t.Fatal("lost valuation paths")
	}
	p.TrainingSeed, p.ValuationSeed = 99, 99
	if _, err := PriceOption(p); err == nil {
		t.Fatal("accepted identical seeds")
	}
}

func TestFrozenPolicyUsesOnlyCurrentState(t *testing.T) {
	p := testPricingInput()
	p.ContractType = putContract
	p.Steps = 2
	p.TimeYears = 1
	policy := exercisePolicy{Models: []continuationModel{{}, {Active: true, Scale: 1, Coefficients: [3]float64{5, 0, 0}}, {}}}
	// Both paths exercise at t=.5 for $10, regardless of their different futures.
	paths := [][]float64{{100, 90, 1}, {100, 90, 200}}
	r, err := evaluateExercisePolicy(p, policy, paths)
	if err != nil {
		t.Fatal(err)
	}
	want := 10 * math.Exp(-p.Rate*.5)
	if math.Abs(r.Price-want) > 1e-12 || r.StandardError != 0 || r.ExerciseCounts[1] != 2 {
		t.Fatalf("future leaked into exercise: %+v", r)
	}
	// Diagnostic floor must not overwrite the fixed-policy estimate or its SE.
	policy.Models[1].Coefficients[0] = 1000
	r, err = evaluateExercisePolicy(p, policy, [][]float64{{100, 90, 110}, {100, 90, 120}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Price != 0 {
		t.Fatal("unexpected raw price")
	}
	p.Spot = 80
	r, err = evaluateExercisePolicy(p, policy, [][]float64{{80, 90, 110}, {80, 90, 120}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Price != 0 || r.BoundAdjustedPrice != 20 || r.BoundAdjustment != 20 {
		t.Fatalf("floor hid raw result: %+v", r)
	}
}

func TestTrainingIndependentOfValuationCount(t *testing.T) {
	p := testPricingInput()
	p.ExerciseStyle = americanStyle
	p.ContractType = putContract
	p.Steps = 10
	p.TrainingPaths = 3000
	p.Simulations = 1000
	a, err := PriceOption(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Simulations = 2000
	b, err := PriceOption(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.TrainingSeed != b.TrainingSeed || a.FallbackRegressions != b.FallbackRegressions || a.NoITMSteps != b.NoITMSteps {
		t.Fatal("valuation configuration affected training")
	}
}

func TestAmericanIndependentBenchmarks(t *testing.T) {
	for _, tc := range []struct {
		name          string
		typ           string
		q, vol, years float64
		steps         int
		biasTolerance float64
	}{
		{"put", putContract, 0, .2, 1, 100, .12},
		{"dividend-call", callContract, .1, .2, 1, 100, .15},
		{"short-low-vol-put", putContract, 0, .01, 7.0 / 365, 20, .0012},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testPricingInput()
			p.ContractType = tc.typ
			p.ExerciseStyle = americanStyle
			p.DividendYield = tc.q
			p.Volatility = tc.vol
			p.TimeYears = tc.years
			p.Steps = tc.steps
			p.TrainingPaths = 30000
			p.Simulations = 30000
			want := americanBinomial(p, 2000)
			for _, seed := range []int64{42, 123} {
				p.Seed = seed
				r, err := PriceOption(p)
				if err != nil {
					t.Fatal(err)
				}
				t.Logf("seed=%d raw=%.6f se=%.6f reference=%.6f adjustment=%.6f fallbacks=%d", seed, r.Price, r.StandardError, want, r.BoundAdjustment, r.FallbackRegressions)
				if math.Abs(r.Price-want) > 4*r.StandardError+tc.biasTolerance {
					t.Fatalf("benchmark deviation beyond sampling + exercise/regression tolerance")
				}
				if r.ConfidenceLow != r.Price-1.959963984540054*r.StandardError {
					t.Fatal("incorrect confidence interval")
				}
			}
		})
	}
}
