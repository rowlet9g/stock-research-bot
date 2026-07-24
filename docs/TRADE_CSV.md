# 거래 CSV 계약

## 현재 상태

`data/trades.normalized.example.csv`는 FORGETMENOT이 정의한 정규화 거래
형식이다. 미래에셋 원본 형식은 2026년 7월에 확인한 `거래내역` XLSX를 기준으로
별도 adapter에서 처리한다.

## 정규화 형식

모든 헤더를 포함한 UTF-8 CSV를 사용한다.

```csv
external_id,trade_date,ticker,action,quantity,price,fees,taxes,currency
DEMO-AAPL-20260702-01,2026-07-02,AAPL,BUY,2,210.50,0.25,0,USD
```

| 열 | 필수 | 규칙 |
| --- | --- | --- |
| `external_id` | 값 필수 | 공급자 안에서 거래를 안정적으로 식별하는 값 |
| `trade_date` | 값 필수 | `YYYY-MM-DD`, `YYYYMMDD`, RFC3339 중 하나 |
| `ticker` | 값 필수 | 먼저 관심종목 DB에 저장된 ticker 또는 Yahoo ticker |
| `action` | 값 필수 | `BUY` 또는 `SELL` |
| `quantity` | 값 필수 | 양수, 소수점 최대 8자리 |
| `price` | 값 필수 | 0 이상, 소수점 최대 8자리 |
| `fees` | 선택 | 빈 값은 0, 그 외 0 이상 |
| `taxes` | 선택 | 빈 값은 0, 그 외 0 이상 |
| `currency` | 값 필수 | `KRW`, `USD` 같은 통화 코드 |

## Import 순서

```powershell
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot trades-import -file data/trades.normalized.example.csv -source mirae-normalized
```

하나의 파일은 `source + file SHA-256`으로 식별한다. 각 거래는
`source + ticker + external_id`로 식별한다.

- 같은 파일을 다시 가져오면 기존 import 결과를 반환한다.
- 다른 파일에 같은 거래가 포함되어도 거래는 다시 저장하지 않는다.
- 한 행이라도 유효하지 않거나 종목이 없으면 전체 파일 import를 rollback한다.
- 원본 CSV 내용은 SQLite에 저장하지 않는다.

## 미래에셋 원본 Adapter

미래에셋 원본 파일은 다음 헤더를 가진 `거래내역` 시트를 사용한다.

```text
거래일자, 거래종류, 종목명, 거래수량, 거래금액, 외화거래금액, 수수료, 예수금잔고
```

```powershell
go run ./cmd/forgetmenot mirae-import -file "C:\path\거래내역.xlsx"
```

체결 행 매핑:

- `주식매수입고`: 국내 `BUY`, `KRW`
- `주식매도출고`: 국내 `SELL`, `KRW`
- `해외주식매수입고`: 해외 `BUY`, `USD`
- `해외주식매도출고`: 해외 `SELL`, `USD`

대응하는 현금 입출금 행은 같은 거래를 중복 표현하므로 거래로 가져오지 않는다.

- `quantity`: `거래수량`
- `price`: 국내 `거래금액 / 거래수량`, 해외 `외화거래금액 / 거래수량`
- `fees`: `수수료`
- `taxes`: 원본에 개별 거래세가 없으므로 0과 함께 `taxes_known=false`
- `trade_date`: `거래일자`, `time_precision=day`
- `external_id`: 날짜, 정규화 종목명, 매매구분, 수량, 금액, 수수료, 통화,
  동일 행 순번으로 생성한 SHA-256 식별자

같은 파일은 파일 SHA-256으로 막고, 기간이 겹치는 다른 파일은 생성한
`external_id`로 중복을 막는다. 원본에 시각과 거래번호가 없으므로 같은 날짜에
모든 값이 동일한 거래가 겹치는 경우에는 원본 행 순서에 따른 occurrence 번호를
사용한다.

종목명은 다음 우선순위로 ticker에 연결한다.

1. SQLite에 저장된 종목명
2. `data/cache/mirae_instrument_aliases.csv`의 검증된 로컬 mapping
3. Yahoo Finance 종목 검색

alias cache에는 `source_url`과 `verified_at`을 기록한다. 실제 계좌 종목 목록이
들어가는 cache와 원본 XLSX는 Git에 커밋하지 않는다. 공개 형식 예시는
`data/mirae_instrument_aliases.example.csv`다.
