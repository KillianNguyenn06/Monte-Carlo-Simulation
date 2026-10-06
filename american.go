package main

import (
	"fmt"
	"math"
)

// A policy is learned once, then evaluated on a separate random stream.
// Models are strike-specific; neither training nor valuation paths are shared across contracts.
type exercisePolicy struct {
	Models              []continuationModel
	ExerciseNow         bool
	FallbackRegressions int
	NoITMSteps          int
}

func americanSeeds(input PricingInput) (int64, int64, error) {
	training, valuation := input.TrainingSeed, input.ValuationSeed
	if training == 0 {
		training = deriveSeed(input.Seed, 0)
	}
	if valuation == 0 {
		valuation = deriveSeed(input.Seed, 1)
	}
	if training == 0 || valuation == 0 || training == valuation {
		return 0, 0, fmt.Errorf("training and valuation seeds must be distinct and nonzero")
	}
	return training, valuation, nil
}

func priceAmericanLSM(input PricingInput) (PricingResult, error) {
	trainingSeed, valuationSeed, err := americanSeeds(input)
	if err != nil {
		return PricingResult{}, err
	}
	trainingInput := input
	trainingInput.Simulations = input.TrainingPaths
	if trainingInput.Simulations == 0 {
		trainingInput.Simulations = defaultTrainingPaths
	}
	trainingInput.Seed = trainingSeed
	trainingPaths, err := assetPriceSim(trainingInput)
	if err != nil {
		return PricingResult{}, err
	}
	policy, err := trainExercisePolicy(input, trainingPaths)
	if err != nil {
		return PricingResult{}, err
	}
	valuationInput := input
	valuationInput.Seed = valuationSeed
	valuationPaths, err := assetPriceSim(valuationInput)
	if err != nil {
		return PricingResult{}, err
	}
	result, err := evaluateExercisePolicy(input, policy, valuationPaths)
	result.TrainingPaths = trainingInput.Simulations
	result.TrainingSeed, result.ValuationSeed = trainingSeed, valuationSeed
	return result, err
}

func trainExercisePolicy(input PricingInput, paths [][]float64) (exercisePolicy, error) {
	policy := exercisePolicy{
		Models: make([]continuationModel, input.Steps+1)}
	dt := input.TimeYears / float64(input.Steps)
	cashflows := make([]float64, len(paths))
	exerciseSteps := make([]int, len(paths))
	for i, path := range paths {
		cashflows[i] = intrinsicValue(path[input.Steps], input.Strike, input.ContractType)
		exerciseSteps[i] = input.Steps
	}
	for step := input.Steps - 1; step >= 1; step-- {
		x := make([]float64, 0, len(paths))
		y := make([]float64, 0, len(paths))
		indices := make([]int, 0, len(paths))
		for i, path := range paths {
			if intrinsicValue(path[step], input.Strike, input.ContractType) <= 0 {
				continue
			}
			x = append(x, path[step]/input.Spot)
			y = append(y, cashflows[i]*math.Exp(-input.Rate*dt*float64(exerciseSteps[i]-step)))
			indices = append(indices, i)
		}
		if len(x) == 0 {
			policy.NoITMSteps++
			continue
		}
		model, err := fitContinuation(x, y)
		if err != nil {
			return exercisePolicy{}, fmt.Errorf("regression at step %d: %w", step, err)
		}
		policy.Models[step] = model
		if model.Fallback {
			policy.FallbackRegressions++
		}
		for j, i := range indices {
			continuation := model.predict(x[j])
			if !isFinite(continuation) {
				return exercisePolicy{}, fmt.Errorf("nonfinite training continuation at step %d", step)
			}
			payoff := intrinsicValue(paths[i][step], input.Strike, input.ContractType)
			if payoff > math.Max(0, continuation) {
				cashflows[i], exerciseSteps[i] = payoff, step
			}
		}
	}
	discounted := make([]float64, len(paths))
	for i := range paths {
		discounted[i] = cashflows[i] * math.Exp(-input.Rate*dt*float64(exerciseSteps[i]))
	}
	continuation, _ := meanAndStandardError(discounted)
	if !isFinite(continuation) {
		return exercisePolicy{}, fmt.Errorf("nonfinite training payoff")
	}
	// Time-zero decision is frozen using training only, never chosen from valuation outcomes.
	policy.ExerciseNow = intrinsicValue(input.Spot, input.Strike, input.ContractType) > continuation
	return policy, nil
}

func evaluateExercisePolicy(input PricingInput, policy exercisePolicy, paths [][]float64) (PricingResult, error) {
	values := make([]float64, len(paths))
	european := make([]float64, len(paths))
	counts := make([]int, input.Steps+1)
	dt := input.TimeYears / float64(input.Steps)
	intrinsic := intrinsicValue(input.Spot, input.Strike, input.ContractType)
	for i, path := range paths {
		exercise := input.Steps
		payoff := intrinsicValue(path[exercise], input.Strike, input.ContractType)
		european[i] = payoff * math.Exp(-input.Rate*input.TimeYears)
		if policy.ExerciseNow {
			exercise, payoff = 0, intrinsic
		} else {
			for step := 1; step < input.Steps; step++ {
				model := policy.Models[step]
				if !model.Active {
					continue
				}
				immediate := intrinsicValue(path[step], input.Strike, input.ContractType)
				if immediate <= 0 {
					continue
				}
				continuation := model.predict(path[step] / input.Spot)
				if !isFinite(continuation) {
					return PricingResult{}, fmt.Errorf("nonfinite valuation continuation at step %d", step)
				}
				if immediate > math.Max(0, continuation) {
					exercise, payoff = step, immediate
					break
				}
			}
		}
		values[i] = payoff * math.Exp(-input.Rate*dt*float64(exercise))
		if !isFinite(values[i]) || !isFinite(european[i]) {
			return PricingResult{}, fmt.Errorf("nonfinite valuation payoff")
		}
		counts[exercise]++ // Final bucket also includes worthless options expiring.
	}
	price, se := meanAndStandardError(values)
	europeanPrice, _ := meanAndStandardError(european)
	adjusted := math.Max(price, math.Max(europeanPrice, intrinsic))
	return PricingResult{Price: price, StandardError: se, SimulatedPaths: len(paths),
		EuropeanPrice: europeanPrice, Intrinsic: intrinsic, BoundAdjustedPrice: adjusted,
		BoundAdjustment: adjusted - price, FallbackRegressions: policy.FallbackRegressions,
		NoITMSteps: policy.NoITMSteps, ExerciseCounts: counts}, nil
}
