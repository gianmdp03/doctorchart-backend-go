package clinical

import (
	"math"
	"math/big"

	"github.com/ALTree/bigfloat"
)

// Round the bounded renal equation's powers once to binary64. Go's Pow loses
// several bits for the integer age factor; the pinned JVM has different rounding.
// Reference captures exercise both sexes and the supported age/creatinine bounds.
func renalPower(x, y float64) float64 {
	if x == 1 {
		return 1
	}
	if x == 0 || math.IsInf(x, 0) || math.IsNaN(x) {
		return math.Pow(x, y)
	}
	value, _ := bigfloat.Pow(new(big.Float).SetPrec(128).SetFloat64(x), new(big.Float).SetPrec(128).SetFloat64(y)).Float64()
	return value
}
