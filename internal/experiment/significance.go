package experiment

import "math"

// ---------------------------------------------------------------------------
// Two-proportion Z-test
// ---------------------------------------------------------------------------

// ZTestResult holds the output of a two-proportion z-test.
type ZTestResult struct {
	ZScore        float64
	PValue        float64
	Significant   bool // p < 0.05
	ControlRate   float64
	TreatmentRate float64
	Lift          float64 // relative improvement %
}

// ZTest performs a two-proportion z-test comparing a control and treatment group.
//
//	z = (p1 - p2) / sqrt(p_pool * (1 - p_pool) * (1/n1 + 1/n2))
func ZTest(controlSuccess, controlTotal, treatmentSuccess, treatmentTotal int64) ZTestResult {
	if controlTotal <= 0 || treatmentTotal <= 0 {
		return ZTestResult{}
	}

	p1 := float64(controlSuccess) / float64(controlTotal)
	p2 := float64(treatmentSuccess) / float64(treatmentTotal)
	pPool := float64(controlSuccess+treatmentSuccess) / float64(controlTotal+treatmentTotal)

	denom := math.Sqrt(pPool * (1 - pPool) * (1.0/float64(controlTotal) + 1.0/float64(treatmentTotal)))
	if denom == 0 {
		return ZTestResult{
			ControlRate:   p1,
			TreatmentRate: p2,
		}
	}

	z := (p2 - p1) / denom
	pValue := 2.0 * (1.0 - normalCDF(math.Abs(z))) // two-tailed

	var lift float64
	if p1 > 0 {
		lift = (p2 - p1) / p1 * 100.0
	}

	return ZTestResult{
		ZScore:        z,
		PValue:        pValue,
		Significant:   pValue < 0.05,
		ControlRate:   p1,
		TreatmentRate: p2,
		Lift:          lift,
	}
}

// ---------------------------------------------------------------------------
// Welch's t-test
// ---------------------------------------------------------------------------

// TTestResult holds the output of Welch's t-test.
type TTestResult struct {
	TStatistic  float64
	PValue      float64
	Significant bool
	MeanDiff    float64
	Lift        float64
}

// WelchTTest performs Welch's t-test comparing two independent samples.
//
//	t = (mean1 - mean2) / sqrt(s1²/n1 + s2²/n2)
//	df = (s1²/n1 + s2²/n2)² / ((s1²/n1)²/(n1-1) + (s2²/n2)²/(n2-1))
func WelchTTest(controlMean, controlStdDev float64, controlN int64,
	treatmentMean, treatmentStdDev float64, treatmentN int64) TTestResult {

	if controlN <= 1 || treatmentN <= 1 {
		return TTestResult{}
	}

	n1 := float64(controlN)
	n2 := float64(treatmentN)
	s1sq := controlStdDev * controlStdDev
	s2sq := treatmentStdDev * treatmentStdDev

	se := math.Sqrt(s1sq/n1 + s2sq/n2)
	if se == 0 {
		return TTestResult{
			MeanDiff: treatmentMean - controlMean,
		}
	}

	t := (treatmentMean - controlMean) / se

	// Welch–Satterthwaite degrees of freedom
	num := (s1sq/n1 + s2sq/n2) * (s1sq/n1 + s2sq/n2)
	denom := (s1sq/n1)*(s1sq/n1)/(n1-1) + (s2sq/n2)*(s2sq/n2)/(n2-1)
	if denom == 0 {
		return TTestResult{
			TStatistic: t,
			MeanDiff:   treatmentMean - controlMean,
		}
	}
	df := num / denom

	pValue := 2.0 * tDistCDF(-math.Abs(t), df) // two-tailed

	var lift float64
	if controlMean != 0 {
		lift = (treatmentMean - controlMean) / math.Abs(controlMean) * 100.0
	}

	return TTestResult{
		TStatistic:  t,
		PValue:      pValue,
		Significant: pValue < 0.05,
		MeanDiff:    treatmentMean - controlMean,
		Lift:        lift,
	}
}

// ---------------------------------------------------------------------------
// Standard normal CDF — Abramowitz & Stegun approximation (formula 26.2.17)
// Maximum error ≈ 7.5×10⁻⁸.
// ---------------------------------------------------------------------------

func normalCDF(x float64) float64 {
	if x < -8 {
		return 0
	}
	if x > 8 {
		return 1
	}

	const (
		a1 = 0.254829592
		a2 = -0.284496736
		a3 = 1.421413741
		a4 = -1.453152027
		a5 = 1.061405429
		p  = 0.3275911
	)

	sign := 1.0
	if x < 0 {
		sign = -1.0
	}
	z := math.Abs(x) / math.Sqrt2

	t := 1.0 / (1.0 + p*z)
	y := 1.0 - (((((a5*t+a4)*t)+a3)*t+a2)*t+a1)*t*math.Exp(-z*z)

	return 0.5 * (1.0 + sign*y)
}

// ---------------------------------------------------------------------------
// Student-t CDF via the regularized incomplete beta function
// ---------------------------------------------------------------------------

// tDistCDF returns P(T <= t) for a t-distribution with df degrees of freedom.
func tDistCDF(t float64, df float64) float64 {
	if df <= 0 {
		return 0.5
	}
	x := df / (df + t*t)
	ibeta := regularizedIncompleteBeta(df/2.0, 0.5, x)
	if t >= 0 {
		return 1.0 - 0.5*ibeta
	}
	return 0.5 * ibeta
}

// regularizedIncompleteBeta computes I_x(a, b) using a continued-fraction
// expansion (Lentz's method). This is accurate for the ranges we need.
func regularizedIncompleteBeta(a, b, x float64) float64 {
	if x < 0 || x > 1 {
		return 0
	}
	if x == 0 {
		return 0
	}
	if x == 1 {
		return 1
	}

	// Use the symmetry relation when x > (a+1)/(a+b+2) for faster convergence.
	if x > (a+1)/(a+b+2) {
		return 1.0 - regularizedIncompleteBeta(b, a, 1.0-x)
	}

	lnBeta := lgamma(a) + lgamma(b) - lgamma(a+b)
	front := math.Exp(math.Log(x)*a + math.Log(1-x)*b - lnBeta) / a

	// Lentz's continued fraction
	const maxIter = 200
	const epsilon = 1e-14
	const tiny = 1e-30

	f := tiny
	c := tiny
	d := 0.0

	for i := 0; i <= maxIter; i++ {
		var an float64
		if i == 0 {
			an = 1.0
		} else {
			m := float64(i)
			if i%2 == 0 {
				// even term
				k := m / 2.0
				an = (k * (b - k) * x) / ((a + 2*k - 1) * (a + 2*k))
			} else {
				// odd term
				k := (m - 1) / 2.0
				an = -((a + k) * (a + b + k) * x) / ((a + 2*k) * (a + 2*k + 1))
			}
		}

		d = 1.0 + an*d
		if math.Abs(d) < tiny {
			d = tiny
		}
		c = 1.0 + an/c
		if math.Abs(c) < tiny {
			c = tiny
		}
		d = 1.0 / d
		delta := c * d
		f *= delta
		if math.Abs(delta-1.0) < epsilon {
			break
		}
	}

	return front * f
}

// lgamma wraps math.Lgamma, ignoring the sign.
func lgamma(x float64) float64 {
	v, _ := math.Lgamma(x)
	return v
}
