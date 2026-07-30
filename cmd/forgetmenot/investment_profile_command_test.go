package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/rowlet9g/stock-research-bot/internal/models"
	sqlitestore "github.com/rowlet9g/stock-research-bot/internal/storage/sqlite"
)

func TestInvestmentProfileSyncIsIdempotent(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "forgetmenot.db")
	store, err := sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("open profile sync store: %v", err)
	}
	if _, err := store.SyncInstruments(ctx, []models.WatchlistItem{{
		Name:        "Apple",
		Ticker:      "AAPL",
		YahooTicker: "AAPL",
		Market:      "NASDAQ",
		Currency:    "USD",
	}}); err != nil {
		t.Fatalf("seed profile instrument: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close profile sync store: %v", err)
	}

	profilePath := filepath.Join(t.TempDir(), "investment-profile.json")
	if err := os.WriteFile(profilePath, []byte(testInvestmentProfileJSON()), 0o600); err != nil {
		t.Fatalf("write investment profile: %v", err)
	}
	firstOutput := runCommand(
		t,
		"investment-profile-sync",
		"-db", databasePath,
		"-file", profilePath,
		"-output", "json",
	)
	var first investmentProfileSyncCommandResult
	if err := json.Unmarshal(firstOutput, &first); err != nil {
		t.Fatalf("decode first profile sync: %v\n%s", err, firstOutput)
	}
	secondOutput := runCommand(
		t,
		"investment-profile-sync",
		"-db", databasePath,
		"-file", profilePath,
		"-output", "json",
	)
	var second investmentProfileSyncCommandResult
	if err := json.Unmarshal(secondOutput, &second); err != nil {
		t.Fatalf("decode second profile sync: %v\n%s", err, secondOutput)
	}
	if first.Sync.RowsChanged != 1 ||
		second.Sync.RowsChanged != 0 ||
		second.Sync.RowsUnchanged != 1 {
		t.Fatalf("profile sync was not idempotent: first=%#v second=%#v", first, second)
	}

	store, err = sqlitestore.Open(ctx, databasePath)
	if err != nil {
		t.Fatalf("reopen profile sync store: %v", err)
	}
	defer store.Close()
	record, err := store.Portfolio(ctx, "AAPL")
	if err != nil {
		t.Fatalf("load synced portfolio: %v", err)
	}
	if record.Thesis == nil ||
		record.Thesis.AllocationCategory != "core" ||
		record.Thesis.ProtectedQuantityUnits != decimal.Scale ||
		record.Thesis.IncreaseCondition != "실적 확인 후 증액" {
		t.Fatalf("unexpected synced thesis: %#v", record.Thesis)
	}
}

func testInvestmentProfileJSON() string {
	return `{
		"version": "investment-profile/v1",
		"portfolio_policy": {
			"objective": "위험을 낮춘 장기 성장",
			"target_annual_return_percent": {
				"minimum_percent": 9,
				"maximum_percent": 12
			},
			"allocations": [
				{
					"category": "core",
					"label": "코어",
					"target_percent": 50,
					"assets": ["SPYM"],
					"guidance": "적립"
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
			"review_rules": ["분기 점검"]
		},
		"theses": [
			{
				"ticker": "AAPL",
				"allocation_category": "core",
				"protected_quantity": "1",
				"summary": "서비스 성장",
				"invalidation_condition": "성장 둔화",
				"increase_condition": "실적 확인 후 증액",
				"expected_holding_period": "12개월",
				"check_metrics": ["서비스 매출"]
			}
		]
	}`
}
