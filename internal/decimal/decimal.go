package decimal

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const Scale int64 = 100_000_000

func Parse(value string) (int64, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, ",", ""))
	if value == "" {
		return 0, fmt.Errorf("decimal value is empty")
	}

	sign := int64(1)
	switch value[0] {
	case '-':
		sign = -1
		value = value[1:]
	case '+':
		value = value[1:]
	}
	if value == "" {
		return 0, fmt.Errorf("decimal value has no digits")
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 {
		return 0, fmt.Errorf("invalid decimal value %q", value)
	}
	wholePart := parts[0]
	fractionPart := ""
	if len(parts) == 2 {
		fractionPart = parts[1]
	}
	if wholePart == "" && fractionPart == "" {
		return 0, fmt.Errorf("decimal value has no digits")
	}
	if wholePart == "" {
		wholePart = "0"
	}
	if !isDigits(wholePart) {
		return 0, fmt.Errorf("invalid decimal value %q", value)
	}

	if !isDigits(fractionPart) {
		return 0, fmt.Errorf("invalid decimal value %q", value)
	}
	if len(fractionPart) > 8 {
		return 0, fmt.Errorf("decimal value %q has more than 8 fractional digits", value)
	}
	fractionPart += strings.Repeat("0", 8-len(fractionPart))

	whole, err := strconv.ParseInt(wholePart, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse decimal whole part: %w", err)
	}
	fraction := int64(0)
	if fractionPart != "" {
		fraction, err = strconv.ParseInt(fractionPart, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse decimal fraction: %w", err)
		}
	}
	if whole > (int64(^uint64(0)>>1)-fraction)/Scale {
		return 0, fmt.Errorf("decimal value %q exceeds int64 range", value)
	}
	return sign * (whole*Scale + fraction), nil
}

func Format(value int64) string {
	sign := ""
	magnitude := uint64(value)
	if value < 0 {
		sign = "-"
		magnitude = uint64(-(value + 1)) + 1
	}
	whole := magnitude / uint64(Scale)
	fraction := magnitude % uint64(Scale)
	if fraction == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	fractionText := strings.TrimRight(fmt.Sprintf("%08d", fraction), "0")
	return fmt.Sprintf("%s%d.%s", sign, whole, fractionText)
}

// Multiply returns left*right rounded half away from zero at Scale precision.
func Multiply(left int64, right int64) (int64, error) {
	product := new(big.Int).Mul(big.NewInt(left), big.NewInt(right))
	divisor := big.NewInt(Scale)
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(product, divisor, remainder)

	absoluteRemainder := new(big.Int).Abs(remainder)
	if absoluteRemainder.Mul(absoluteRemainder, big.NewInt(2)).Cmp(divisor) >= 0 {
		if (left < 0) == (right < 0) {
			quotient.Add(quotient, big.NewInt(1))
		} else {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("decimal multiplication result exceeds int64 range")
	}
	return quotient.Int64(), nil
}

// Divide returns dividend/divisor rounded half away from zero at Scale precision.
func Divide(dividend int64, divisor int64) (int64, error) {
	if divisor == 0 {
		return 0, fmt.Errorf("decimal divisor must not be zero")
	}

	numerator := new(big.Int).Mul(big.NewInt(dividend), big.NewInt(Scale))
	denominator := big.NewInt(divisor)
	quotient := new(big.Int)
	remainder := new(big.Int)
	quotient.QuoRem(numerator, denominator, remainder)

	absoluteRemainder := new(big.Int).Abs(remainder)
	absoluteDenominator := new(big.Int).Abs(new(big.Int).Set(denominator))
	if absoluteRemainder.Mul(absoluteRemainder, big.NewInt(2)).Cmp(absoluteDenominator) >= 0 {
		sameSign := (dividend < 0) == (divisor < 0)
		if sameSign {
			quotient.Add(quotient, big.NewInt(1))
		} else {
			quotient.Sub(quotient, big.NewInt(1))
		}
	}
	if !quotient.IsInt64() {
		return 0, fmt.Errorf("decimal division result exceeds int64 range")
	}
	return quotient.Int64(), nil
}

func isDigits(value string) bool {
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}
