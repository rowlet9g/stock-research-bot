package investmentprofile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

const (
	Version      = "investment-profile/v1"
	maxFileBytes = 1024 * 1024
)

type ReturnTarget struct {
	MinimumPercent int `json:"minimum_percent"`
	MaximumPercent int `json:"maximum_percent"`
}

type Allocation struct {
	Category      string   `json:"category"`
	Label         string   `json:"label"`
	TargetPercent int      `json:"target_percent"`
	Assets        []string `json:"assets"`
	Guidance      string   `json:"guidance"`
}

type Policy struct {
	Objective                 string       `json:"objective"`
	TargetAnnualReturnPercent ReturnTarget `json:"target_annual_return_percent"`
	Allocations               []Allocation `json:"allocations"`
	ReviewRules               []string     `json:"review_rules"`
}

type Thesis struct {
	Ticker                 string   `json:"ticker"`
	AllocationCategory     string   `json:"allocation_category"`
	ProtectedQuantity      string   `json:"protected_quantity"`
	Summary                string   `json:"summary"`
	InvalidationCondition  string   `json:"invalidation_condition"`
	IncreaseCondition      string   `json:"increase_condition"`
	ExpectedHoldingPeriod  string   `json:"expected_holding_period"`
	CheckMetrics           []string `json:"check_metrics"`
	ProtectedQuantityUnits int64    `json:"-"`
}

type Profile struct {
	Version         string   `json:"version"`
	PortfolioPolicy Policy   `json:"portfolio_policy"`
	Theses          []Thesis `json:"theses"`
}

func Load(path string) (Profile, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Profile{}, fmt.Errorf("investment profile path is required")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, fmt.Errorf("read investment profile %q: %w", path, err)
	}
	if len(content) > maxFileBytes {
		return Profile{}, fmt.Errorf(
			"investment profile %q exceeds %d bytes",
			path,
			maxFileBytes,
		)
	}
	profile, err := Parse(content)
	if err != nil {
		return Profile{}, fmt.Errorf("parse investment profile %q: %w", path, err)
	}
	return profile, nil
}

func LoadIfExists(path string) (Profile, bool, error) {
	profile, err := Load(path)
	if err == nil {
		return profile, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, false, nil
	}
	return Profile{}, false, err
}

func Parse(content []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()

	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return Profile{}, fmt.Errorf("investment profile contains trailing JSON")
		}
		return Profile{}, fmt.Errorf("decode trailing investment profile content: %w", err)
	}
	if err := normalizeAndValidate(&profile); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func normalizeAndValidate(profile *Profile) error {
	profile.Version = strings.TrimSpace(profile.Version)
	if profile.Version != Version {
		return fmt.Errorf(
			"investment profile version must be %q, got %q",
			Version,
			profile.Version,
		)
	}
	if err := normalizeAndValidatePolicy(&profile.PortfolioPolicy); err != nil {
		return err
	}

	categories := make(map[string]struct{}, len(profile.PortfolioPolicy.Allocations))
	for _, allocation := range profile.PortfolioPolicy.Allocations {
		categories[strings.ToLower(allocation.Category)] = struct{}{}
	}
	seenTickers := make(map[string]struct{}, len(profile.Theses))
	for index := range profile.Theses {
		thesis := &profile.Theses[index]
		thesis.Ticker = strings.ToUpper(strings.TrimSpace(thesis.Ticker))
		thesis.AllocationCategory = strings.ToLower(
			strings.TrimSpace(thesis.AllocationCategory),
		)
		thesis.ProtectedQuantity = strings.TrimSpace(thesis.ProtectedQuantity)
		thesis.Summary = strings.TrimSpace(thesis.Summary)
		thesis.InvalidationCondition = strings.TrimSpace(
			thesis.InvalidationCondition,
		)
		thesis.IncreaseCondition = strings.TrimSpace(thesis.IncreaseCondition)
		thesis.ExpectedHoldingPeriod = strings.TrimSpace(
			thesis.ExpectedHoldingPeriod,
		)
		thesis.CheckMetrics = normalizeStrings(thesis.CheckMetrics, false)

		if thesis.Ticker == "" {
			return fmt.Errorf("thesis %d ticker is required", index+1)
		}
		if _, exists := seenTickers[thesis.Ticker]; exists {
			return fmt.Errorf("thesis %d duplicates ticker %q", index+1, thesis.Ticker)
		}
		seenTickers[thesis.Ticker] = struct{}{}
		if _, exists := categories[thesis.AllocationCategory]; !exists {
			return fmt.Errorf(
				"thesis %d ticker %q uses unknown allocation category %q",
				index+1,
				thesis.Ticker,
				thesis.AllocationCategory,
			)
		}
		if thesis.Summary == "" {
			return fmt.Errorf(
				"thesis %d ticker %q summary is required",
				index+1,
				thesis.Ticker,
			)
		}
		if thesis.ProtectedQuantity == "" {
			thesis.ProtectedQuantity = "0"
		}
		units, err := decimal.Parse(thesis.ProtectedQuantity)
		if err != nil {
			return fmt.Errorf(
				"thesis %d ticker %q has invalid protected quantity: %w",
				index+1,
				thesis.Ticker,
				err,
			)
		}
		if units < 0 {
			return fmt.Errorf(
				"thesis %d ticker %q protected quantity must not be negative",
				index+1,
				thesis.Ticker,
			)
		}
		thesis.ProtectedQuantityUnits = units
	}
	return nil
}

func normalizeAndValidatePolicy(policy *Policy) error {
	policy.Objective = strings.TrimSpace(policy.Objective)
	if policy.Objective == "" {
		return fmt.Errorf("portfolio policy objective is required")
	}
	target := policy.TargetAnnualReturnPercent
	if target.MinimumPercent <= 0 ||
		target.MaximumPercent < target.MinimumPercent ||
		target.MaximumPercent > 100 {
		return fmt.Errorf(
			"target annual return percent must satisfy 0 < minimum <= maximum <= 100",
		)
	}
	if len(policy.Allocations) == 0 {
		return fmt.Errorf("portfolio policy allocations are required")
	}

	totalPercent := 0
	seenCategories := make(map[string]struct{}, len(policy.Allocations))
	for index := range policy.Allocations {
		allocation := &policy.Allocations[index]
		allocation.Category = strings.ToLower(strings.TrimSpace(allocation.Category))
		allocation.Label = strings.TrimSpace(allocation.Label)
		allocation.Assets = normalizeStrings(allocation.Assets, true)
		allocation.Guidance = strings.TrimSpace(allocation.Guidance)
		if allocation.Category == "" {
			return fmt.Errorf("allocation %d category is required", index+1)
		}
		if _, exists := seenCategories[allocation.Category]; exists {
			return fmt.Errorf(
				"allocation %d duplicates category %q",
				index+1,
				allocation.Category,
			)
		}
		seenCategories[allocation.Category] = struct{}{}
		if allocation.Label == "" {
			return fmt.Errorf("allocation %d label is required", index+1)
		}
		if allocation.TargetPercent <= 0 || allocation.TargetPercent > 100 {
			return fmt.Errorf(
				"allocation %d target percent must be between 1 and 100",
				index+1,
			)
		}
		totalPercent += allocation.TargetPercent
	}
	if totalPercent != 100 {
		return fmt.Errorf(
			"portfolio policy allocation target percent must total 100, got %d",
			totalPercent,
		)
	}
	policy.ReviewRules = normalizeStrings(policy.ReviewRules, false)
	return nil
}

func normalizeStrings(values []string, uppercase bool) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if uppercase {
			value = strings.ToUpper(value)
		}
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}
