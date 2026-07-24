package mirae

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/rowlet9g/stock-research-bot/internal/decimal"
	"github.com/xuri/excelize/v2"
)

var ledgerHeaders = []string{
	"거래일자",
	"거래종류",
	"종목명",
	"거래수량",
	"거래금액",
	"외화거래금액",
	"수수료",
	"예수금잔고",
}

type Trade struct {
	RowNumber      int
	InstrumentName string
	TradeDate      time.Time
	Action         string
	QuantityUnits  int64
	PriceUnits     int64
	FeesUnits      int64
	TaxesUnits     int64
	Currency       string
	ExternalID     string
	PriceSource    string
	TaxesKnown     bool
	TimePrecision  string
}

type Ledger struct {
	FileSHA256   string
	SheetName    string
	RowsSeen     int
	RowsSkipped  int
	Trades       []Trade
	IgnoredTypes map[string]int
	Warnings     []string
}

type tradeDefinition struct {
	action      string
	currency    string
	amountField string
}

var tradeDefinitions = map[string]tradeDefinition{
	"주식매수입고": {
		action:      "BUY",
		currency:    "KRW",
		amountField: "거래금액",
	},
	"주식매도출고": {
		action:      "SELL",
		currency:    "KRW",
		amountField: "거래금액",
	},
	"해외주식매수입고": {
		action:      "BUY",
		currency:    "USD",
		amountField: "외화거래금액",
	},
	"해외주식매도출고": {
		action:      "SELL",
		currency:    "USD",
		amountField: "외화거래금액",
	},
}

func LoadXLSX(path string) (Ledger, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return Ledger{}, fmt.Errorf("read Mirae Asset ledger %q: %w", path, err)
	}
	ledger, err := ParseXLSX(content)
	if err != nil {
		return Ledger{}, fmt.Errorf("parse Mirae Asset ledger %q: %w", path, err)
	}
	sum := sha256.Sum256(content)
	ledger.FileSHA256 = hex.EncodeToString(sum[:])
	return ledger, nil
}

func ParseXLSX(content []byte) (Ledger, error) {
	workbook, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		return Ledger{}, fmt.Errorf("open XLSX: %w", err)
	}
	defer workbook.Close()

	sheetName, rows, headerRow, header, err := findLedgerSheet(workbook)
	if err != nil {
		return Ledger{}, err
	}
	ledger := Ledger{
		SheetName:    sheetName,
		Trades:       []Trade{},
		IgnoredTypes: map[string]int{},
		Warnings: []string{
			"execution time is unavailable; trade_date has day precision",
			"unit price is derived from transaction amount divided by quantity",
			"per-trade taxes are not provided by this ledger",
			"external_id is derived from stable trade fields",
		},
	}
	occurrences := map[string]int{}
	for rowIndex := headerRow + 1; rowIndex < len(rows); rowIndex++ {
		row := rows[rowIndex]
		if blankRow(row) {
			continue
		}
		ledger.RowsSeen++

		transactionType := field(row, header, "거래종류")
		definition, isTrade := tradeDefinitions[transactionType]
		if !isTrade {
			ledger.RowsSkipped++
			ledger.IgnoredTypes[transactionType]++
			continue
		}
		trade, fingerprint, err := parseTradeRow(row, rowIndex+1, header, definition)
		if err != nil {
			return Ledger{}, err
		}
		occurrences[fingerprint]++
		trade.ExternalID = generatedExternalID(fingerprint, occurrences[fingerprint])
		ledger.Trades = append(ledger.Trades, trade)
	}
	if len(ledger.Trades) == 0 {
		return Ledger{}, fmt.Errorf("sheet %q contains no supported trade rows", sheetName)
	}
	return ledger, nil
}

func findLedgerSheet(
	workbook *excelize.File,
) (string, [][]string, int, map[string]int, error) {
	for _, sheetName := range workbook.GetSheetList() {
		rows, err := workbook.GetRows(sheetName)
		if err != nil {
			return "", nil, 0, nil, fmt.Errorf("read sheet %q: %w", sheetName, err)
		}
		for rowIndex, row := range rows {
			header, ok := parseHeader(row)
			if ok {
				return sheetName, rows, rowIndex, header, nil
			}
		}
	}
	return "", nil, 0, nil, fmt.Errorf(
		"no sheet contains the required Mirae Asset ledger headers",
	)
}

func parseHeader(row []string) (map[string]int, bool) {
	indexes := map[string]int{}
	for index, value := range row {
		name := strings.TrimSpace(value)
		if name == "" {
			continue
		}
		if _, exists := indexes[name]; exists {
			return nil, false
		}
		indexes[name] = index
	}
	for _, name := range ledgerHeaders {
		if _, exists := indexes[name]; !exists {
			return nil, false
		}
	}
	return indexes, true
}

func parseTradeRow(
	row []string,
	rowNumber int,
	header map[string]int,
	definition tradeDefinition,
) (Trade, string, error) {
	instrumentName := field(row, header, "종목명")
	if instrumentName == "" {
		return Trade{}, "", fmt.Errorf("Mirae Asset ledger row %d: 종목명 is required", rowNumber)
	}
	tradeDate, err := time.Parse("2006.01.02", field(row, header, "거래일자"))
	if err != nil {
		return Trade{}, "", fmt.Errorf(
			"Mirae Asset ledger row %d: invalid 거래일자: %w",
			rowNumber,
			err,
		)
	}
	quantityUnits, err := positiveDecimal(field(row, header, "거래수량"))
	if err != nil {
		return Trade{}, "", fmt.Errorf(
			"Mirae Asset ledger row %d: invalid 거래수량: %w",
			rowNumber,
			err,
		)
	}
	amountUnits, err := positiveDecimal(field(row, header, definition.amountField))
	if err != nil {
		return Trade{}, "", fmt.Errorf(
			"Mirae Asset ledger row %d: invalid %s: %w",
			rowNumber,
			definition.amountField,
			err,
		)
	}
	feesUnits, err := nonNegativeDecimal(field(row, header, "수수료"))
	if err != nil {
		return Trade{}, "", fmt.Errorf(
			"Mirae Asset ledger row %d: invalid 수수료: %w",
			rowNumber,
			err,
		)
	}
	priceUnits, err := decimal.Divide(amountUnits, quantityUnits)
	if err != nil {
		return Trade{}, "", fmt.Errorf(
			"Mirae Asset ledger row %d: derive unit price: %w",
			rowNumber,
			err,
		)
	}

	trade := Trade{
		RowNumber:      rowNumber,
		InstrumentName: instrumentName,
		TradeDate:      tradeDate.UTC(),
		Action:         definition.action,
		QuantityUnits:  quantityUnits,
		PriceUnits:     priceUnits,
		FeesUnits:      feesUnits,
		TaxesUnits:     0,
		Currency:       definition.currency,
		PriceSource:    "derived_amount_div_quantity",
		TaxesKnown:     false,
		TimePrecision:  "day",
	}
	fingerprint := strings.Join([]string{
		trade.TradeDate.Format("2006-01-02"),
		NormalizeInstrumentName(trade.InstrumentName),
		trade.Action,
		decimal.Format(trade.QuantityUnits),
		decimal.Format(amountUnits),
		decimal.Format(trade.FeesUnits),
		trade.Currency,
	}, "\x1f")
	return trade, fingerprint, nil
}

func NormalizeInstrumentName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "보통주")

	var normalized strings.Builder
	for _, char := range strings.ToUpper(value) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(char)
		}
	}
	return normalized.String()
}

func generatedExternalID(fingerprint string, occurrence int) string {
	sum := sha256.Sum256([]byte(fingerprint))
	return fmt.Sprintf("mirae-ledger:%s:%d", hex.EncodeToString(sum[:]), occurrence)
}

func positiveDecimal(value string) (int64, error) {
	parsed, err := decimal.Parse(value)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		parsed = -parsed
	}
	if parsed == 0 {
		return 0, fmt.Errorf("value must be greater than zero")
	}
	return parsed, nil
}

func nonNegativeDecimal(value string) (int64, error) {
	parsed, err := decimal.Parse(value)
	if err != nil {
		return 0, err
	}
	if parsed < 0 {
		parsed = -parsed
	}
	return parsed, nil
}

func field(row []string, header map[string]int, name string) string {
	index := header[name]
	if index >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[index])
}

func blankRow(row []string) bool {
	for _, value := range row {
		if strings.TrimSpace(value) != "" {
			return false
		}
	}
	return true
}
