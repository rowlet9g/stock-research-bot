package investmentprofile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
)

func TestParseNormalizesValidProfile(t *testing.T) {
	profile, err := Parse([]byte(`{
		"version": "investment-profile/v2",
		"portfolio_policy": {
			"objective": " 위험을 낮춘 장기 성장 ",
			"target_annual_return_percent": {
				"minimum_percent": 9,
				"maximum_percent": 12
			},
			"allocations": [
				{
					"category": " CORE ",
					"label": "코어",
					"target_percent": 50,
					"assets": ["spym", "SPYM", "vti"],
					"guidance": "적립식"
				},
				{
					"category": "growth",
					"label": "성장",
					"target_percent": 20,
					"assets": [],
					"guidance": "상한 관리"
				},
				{
					"category": "defensive",
					"label": "방어",
					"target_percent": 30,
					"assets": [],
					"guidance": "변동성 완화"
				}
			],
			"rebalance_policy": {
				"mode": " CASH_FLOW_FIRST ",
				"preferred_max_realized_loss_percent": 7,
				"hard_max_realized_loss_percent": 10,
				"realized_loss_limit_basis": " EACH_POSITION_COST_BASIS ",
				"thesis_invalidation_overrides_limit": true,
				"max_turnover_percent": 100,
				"target_horizon_months": 3,
				"force_target_allocation_by_deadline": false
			},
			"review_rules": ["분기 점검", "분기 점검"]
		},
		"theses": [
			{
				"ticker": " aapl ",
				"allocation_category": "CORE",
				"protected_quantity": "1",
				"summary": "서비스 성장",
				"invalidation_condition": "성장 둔화",
				"increase_condition": "실적 확인",
				"expected_holding_period": "12개월",
				"check_metrics": ["매출", "매출"]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("parse valid profile: %v", err)
	}
	if profile.PortfolioPolicy.Objective != "위험을 낮춘 장기 성장" ||
		len(profile.PortfolioPolicy.ReviewRules) != 1 ||
		len(profile.PortfolioPolicy.Allocations[0].Assets) != 2 ||
		profile.PortfolioPolicy.RebalancePolicy.Mode !=
			RebalanceModeCashFlowFirst ||
		profile.PortfolioPolicy.RebalancePolicy.RealizedLossLimitBasis !=
			RealizedLossBasisPosition {
		t.Fatalf("policy was not normalized: %#v", profile.PortfolioPolicy)
	}
	thesis := profile.Theses[0]
	if thesis.Ticker != "AAPL" ||
		thesis.AllocationCategory != "core" ||
		thesis.ProtectedQuantityUnits != decimal.Scale ||
		len(thesis.CheckMetrics) != 1 {
		t.Fatalf("thesis was not normalized: %#v", thesis)
	}
}

func TestParseRejectsUnknownField(t *testing.T) {
	_, err := Parse([]byte(`{"version":"investment-profile/v2","unknown":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown field error, got %v", err)
	}
}

func TestParseRejectsInvalidAllocationTotal(t *testing.T) {
	_, err := Parse([]byte(minimalProfileJSON(90, "0", "core")))
	if err == nil || !strings.Contains(err.Error(), "must total 100") {
		t.Fatalf("expected allocation total error, got %v", err)
	}
}

func TestParseRejectsInvalidProtectedQuantity(t *testing.T) {
	_, err := Parse([]byte(minimalProfileJSON(100, "-1", "core")))
	if err == nil || !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("expected protected quantity error, got %v", err)
	}
}

func TestParseRejectsUnknownThesisCategory(t *testing.T) {
	_, err := Parse([]byte(minimalProfileJSON(100, "0", "growth")))
	if err == nil || !strings.Contains(err.Error(), "unknown allocation category") {
		t.Fatalf("expected unknown category error, got %v", err)
	}
}

func TestParseRejectsInvalidRebalanceLossLimits(t *testing.T) {
	content := strings.Replace(
		minimalProfileJSON(100, "0", "core"),
		`"preferred_max_realized_loss_percent": 7`,
		`"preferred_max_realized_loss_percent": 11`,
		1,
	)
	_, err := Parse([]byte(content))
	if err == nil || !strings.Contains(err.Error(), "preferred <= hard") {
		t.Fatalf("expected rebalance loss limit error, got %v", err)
	}
}

func minimalProfileJSON(
	targetPercent int,
	protectedQuantity string,
	thesisCategory string,
) string {
	return `{
		"version": "investment-profile/v2",
		"portfolio_policy": {
			"objective": "장기 성장",
			"target_annual_return_percent": {
				"minimum_percent": 9,
				"maximum_percent": 12
			},
			"allocations": [{
				"category": "core",
				"label": "코어",
				"target_percent": ` + fmt.Sprint(targetPercent) + `,
				"assets": ["SPYM"],
				"guidance": "적립"
			}],
			"rebalance_policy": {
				"mode": "cash_flow_first",
				"preferred_max_realized_loss_percent": 7,
				"hard_max_realized_loss_percent": 10,
				"realized_loss_limit_basis": "each_position_cost_basis",
				"thesis_invalidation_overrides_limit": true,
				"max_turnover_percent": 100,
				"target_horizon_months": 3,
				"force_target_allocation_by_deadline": false
			},
			"review_rules": []
		},
		"theses": [{
			"ticker": "AAPL",
			"allocation_category": "` + thesisCategory + `",
			"protected_quantity": "` + protectedQuantity + `",
			"summary": "성장",
			"invalidation_condition": "",
			"increase_condition": "",
			"expected_holding_period": "",
			"check_metrics": []
		}]
	}`
}
