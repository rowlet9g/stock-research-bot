package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/rowlet9g/stock-research-bot/internal/analysis"
	"github.com/rowlet9g/stock-research-bot/internal/config"
	"github.com/rowlet9g/stock-research-bot/internal/dart"
	"github.com/rowlet9g/stock-research-bot/internal/prompt"
	"github.com/rowlet9g/stock-research-bot/internal/watchlist"
	"github.com/rowlet9g/stock-research-bot/internal/yahoo"
)

func main() {
	watchlistPath := flag.String("watchlist", "data/watchlist.example.csv", "watchlist CSV path")
	name := flag.String("name", "", "stock name to analyze")
	ticker := flag.String("ticker", "", "ticker or Yahoo ticker to analyze")
	thesis := flag.String("thesis", "", "user investment thesis")
	days := flag.Int("days", 30, "DART disclosure lookback days")
	flag.Parse()

	settings := config.Load(".env")

	items, err := watchlist.Load(*watchlistPath)
	if err != nil {
		log.Fatalf("load watchlist: %v", err)
	}
	if len(items) == 0 {
		log.Fatalf("watchlist is empty: %s", *watchlistPath)
	}

	selected, err := watchlist.Select(items, *name, *ticker)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	snapshot, err := yahoo.BuildPriceSnapshot(ctx, selected.YahooTicker)
	if err != nil {
		log.Printf("Yahoo price fetch failed: %v", err)
		snapshot = yahoo.EmptyPriceSnapshot(selected.YahooTicker)
	}
	signals := analysis.EvaluatePriceSnapshot(snapshot)

	disclosures := []dart.Disclosure{}
	if settings.OpenDARTAPIKey != "" && strings.TrimSpace(selected.DARTCorpCode) != "" {
		client := dart.NewClient(settings.OpenDARTAPIKey)
		disclosures, err = client.RecentDisclosures(ctx, selected.DARTCorpCode, *days, 20)
		if err != nil {
			log.Printf("DART disclosure fetch failed: %v", err)
		}
	}

	fmt.Printf("Selected: %s (%s / %s)\n", selected.Name, selected.Ticker, selected.YahooTicker)
	fmt.Printf("Last price: %s\n", formatFloat(snapshot.LastPrice))
	fmt.Printf("1D change: %s%%\n", formatFloat(snapshot.ChangePct1D))
	fmt.Printf("MA20: %s\n", formatFloat(snapshot.MA20))
	fmt.Printf("MA60: %s\n", formatFloat(snapshot.MA60))
	fmt.Printf("Volume: %s\n\n", formatInt(snapshot.Volume))

	fmt.Println("Signals:")
	if len(signals) == 0 {
		fmt.Println("- 특이 신호 없음")
	}
	for _, signal := range signals {
		fmt.Printf("- [%s] %s: %s\n", signal.Level, signal.Title, signal.Detail)
	}

	fmt.Println("\nRecent DART disclosures:")
	if len(disclosures) == 0 {
		fmt.Println("- 제공된 공시 없음")
	}
	for _, item := range disclosures {
		fmt.Printf("- %s %s: %s (%s)\n", item.ReceiptDate, item.CorpName, item.ReportName, item.ReceiptNo)
	}

	briefPrompt := prompt.BuildStockBriefPrompt(prompt.StockBriefInput{
		Name:        selected.Name,
		Snapshot:    snapshot,
		Signals:     signals,
		Disclosures: disclosures,
		UserThesis:  *thesis,
	})

	fmt.Println("\n--- ChatGPT Prompt ---")
	fmt.Println(briefPrompt)
}

func formatFloat(value *float64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%.2f", *value)
}

func formatInt(value *int64) string {
	if value == nil {
		return "N/A"
	}
	return fmt.Sprintf("%d", *value)
}

func init() {
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
}
