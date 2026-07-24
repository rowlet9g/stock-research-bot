package mirae

import (
	"strings"
	"testing"
)

func TestParseInstrumentAliases(t *testing.T) {
	input := `source_name,ticker,yahoo_ticker,market,currency,source_url,verified_at
예시전자보통주,123456,123456.KS,KOSPI,KRW,https://example.com/123456,2026-07-24
EXAMPLE ETF,EXMP,EXMP,NASDAQ,USD,https://example.com/exmp,2026-07-24
`
	aliases, err := ParseInstrumentAliases(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse aliases: %v", err)
	}
	if len(aliases) != 2 {
		t.Fatalf("expected 2 aliases, got %d", len(aliases))
	}
	if aliases[0].Instrument.Ticker != "123456" {
		t.Fatalf("unexpected ticker: %q", aliases[0].Instrument.Ticker)
	}
}

func TestParseInstrumentAliasesRejectsDuplicateSourceName(t *testing.T) {
	input := `source_name,ticker,yahoo_ticker,market,currency,source_url,verified_at
예시전자보통주,123456,123456.KS,KOSPI,KRW,https://example.com/123456,2026-07-24
예시전자,654321,654321.KS,KOSPI,KRW,https://example.com/654321,2026-07-24
`
	if _, err := ParseInstrumentAliases(strings.NewReader(input)); err == nil {
		t.Fatal("expected duplicate alias error")
	}
}
