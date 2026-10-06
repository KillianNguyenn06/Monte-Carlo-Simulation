package main

import (
	"fmt"
	"math"
)

type continuationModel struct {
	Coefficients     [3]float64
	Center, Scale    float64
	Active, Fallback bool
}

func (m continuationModel) predict(x float64) float64 {
	z := (x - m.Center) / m.Scale
	return m.Coefficients[0] + z*(m.Coefficients[1]+z*m.Coefficients[2])
}

// Centered/scaled QR with two-pass modified Gram-Schmidt avoids normal equations.
// Coefficients stay in the standardized basis to avoid cancellation near x=1.
func fitContinuation(x, y []float64) (continuationModel, error) {
	if len(x) == 0 || len(x) != len(y) {
		return continuationModel{}, fmt.Errorf("invalid regression samples")
	}
	for i := range x {
		if !isFinite(x[i]) || !isFinite(y[i]) {
			return continuationModel{}, fmt.Errorf("nonfinite regression sample")
		}
	}
	center, _ := meanAndStandardError(x)
	meanY, _ := meanAndStandardError(y)
	fallback := continuationModel{Coefficients: [3]float64{meanY, 0, 0}, Scale: 1, Active: true, Fallback: true}
	if !isFinite(center) || !isFinite(meanY) {
		return continuationModel{}, fmt.Errorf("nonfinite regression mean")
	}
	var scale float64
	for _, v := range x {
		scale = math.Max(scale, math.Abs(v-center))
	}
	if len(x) < 3 || scale <= 1e-14*math.Max(1, math.Abs(center)) {
		return fallback, nil
	}
	q := [3][]float64{make([]float64, len(x)), make([]float64, len(x)), make([]float64, len(x))}
	for i, v := range x {
		z := (v - center) / scale
		q[0][i], q[1][i], q[2][i] = 1, z, z*z
	}
	var r [3][3]float64
	for j := 0; j < 3; j++ {
		for pass := 0; pass < 2; pass++ {
			for k := 0; k < j; k++ {
				var dot float64
				for i := range x {
					dot += q[k][i] * q[j][i]
				}
				r[k][j] += dot
				for i := range x {
					q[j][i] -= dot * q[k][i]
				}
			}
		}
		var norm float64
		for _, v := range q[j] {
			norm += v * v
		}
		norm = math.Sqrt(norm)
		if norm <= 1e-10*math.Sqrt(float64(len(x))) {
			return fallback, nil
		}
		r[j][j] = norm
		for i := range x {
			q[j][i] /= norm
		}
	}
	var coefficients [3]float64
	for j := 2; j >= 0; j-- {
		var rhs float64
		for i := range y {
			rhs += q[j][i] * y[i]
		}
		for k := j + 1; k < 3; k++ {
			rhs -= r[j][k] * coefficients[k]
		}
		coefficients[j] = rhs / r[j][j]
		if !isFinite(coefficients[j]) {
			return continuationModel{}, fmt.Errorf("nonfinite regression coefficient")
		}
	}
	return continuationModel{Coefficients: coefficients, Center: center, Scale: scale, Active: true}, nil
}
