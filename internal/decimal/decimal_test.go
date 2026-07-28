package decimal

import (
	"math"
	"testing"
)

func TestParseAndFormat(t *testing.T) {
	tests := map[string]string{
		"0":            "0",
		"10":           "10",
		"60,000.25":    "60000.25",
		"0.00000001":   "0.00000001",
		"-12.34000000": "-12.34",
	}

	for input, expected := range tests {
		value, err := Parse(input)
		if err != nil {
			t.Fatalf("parse %q: %v", input, err)
		}
		if got := Format(value); got != expected {
			t.Fatalf("format %q: expected %q, got %q", input, expected, got)
		}
	}
}

func TestParseRejectsExcessPrecision(t *testing.T) {
	if _, err := Parse("1.000000001"); err == nil {
		t.Fatal("expected precision error")
	}
}

func TestFormatHandlesMinimumInt64(t *testing.T) {
	if got := Format(math.MinInt64); got != "-92233720368.54775808" {
		t.Fatalf("unexpected minimum int64 format: %s", got)
	}
}

func TestMultiplyRoundsAtScalePrecision(t *testing.T) {
	tests := map[string]struct {
		left     string
		right    string
		expected string
	}{
		"exact": {
			left:     "2.5",
			right:    "210.4",
			expected: "526",
		},
		"rounds positive": {
			left:     "0.00000001",
			right:    "0.5",
			expected: "0.00000001",
		},
		"rounds negative": {
			left:     "-0.00000001",
			right:    "0.5",
			expected: "-0.00000001",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			left := mustParse(t, test.left)
			right := mustParse(t, test.right)
			result, err := Multiply(left, right)
			if err != nil {
				t.Fatalf("multiply: %v", err)
			}
			if got := Format(result); got != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, got)
			}
		})
	}
}

func TestMultiplyRejectsOverflow(t *testing.T) {
	if _, err := Multiply(math.MaxInt64, 2*Scale); err == nil {
		t.Fatal("expected multiplication overflow")
	}
}

func TestDivideRoundsAtScalePrecision(t *testing.T) {
	tests := map[string]struct {
		dividend string
		divisor  string
		expected string
	}{
		"exact": {
			dividend: "421",
			divisor:  "2",
			expected: "210.5",
		},
		"rounds positive": {
			dividend: "2",
			divisor:  "3",
			expected: "0.66666667",
		},
		"rounds negative": {
			dividend: "-2",
			divisor:  "3",
			expected: "-0.66666667",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			dividend := mustParse(t, test.dividend)
			divisor := mustParse(t, test.divisor)
			result, err := Divide(dividend, divisor)
			if err != nil {
				t.Fatalf("divide: %v", err)
			}
			if got := Format(result); got != test.expected {
				t.Fatalf("expected %q, got %q", test.expected, got)
			}
		})
	}
}

func TestDivideRejectsZeroDivisor(t *testing.T) {
	if _, err := Divide(Scale, 0); err == nil {
		t.Fatal("expected zero divisor error")
	}
}

func mustParse(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := Parse(value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}
