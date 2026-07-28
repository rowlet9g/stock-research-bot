# 현재 포지션 CSV

`positions-import`는 증권사 잔고 또는 사용자가 확인한 현재 포지션 스냅샷을
SQLite `positions`에 일괄 저장합니다.

## 실행

```powershell
go run ./cmd/forgetmenot positions-import -file data/positions.csv
go run ./cmd/forgetmenot position-reconcile -output json
```

실제 파일은 `data/*.csv` 규칙으로 Git에서 제외됩니다. 저장소에는
`data/positions.example.csv`만 예제로 포함합니다.

## 헤더

```csv
ticker,quantity,average_cost,currency,as_of
005930,5,250000,KRW,2026-07-28
AAPL,2,210.50,USD,2026-07-28
```

| 필드 | 의미 | 규칙 |
| --- | --- | --- |
| `ticker` | 저장된 종목 코드 | `watchlist-sync` 또는 거래 import로 먼저 등록되어 있어야 함 |
| `quantity` | 기준시각의 보유수량 | 소수점 8자리까지, 0과 음수 허용 |
| `average_cost` | 평균 취득단가 | 소수점 8자리까지, 0 이상 |
| `currency` | 단가 통화 | 비어 있을 수 없으며 저장 시 대문자로 정규화 |
| `as_of` | 잔고 기준시각 | `YYYY-MM-DD` 또는 RFC3339 |

수량 0은 해당 기준시각에 보유하지 않았다는 스냅샷으로 저장할 수 있습니다. 음수는
공매도 포지션을 표현할 수 있게 허용하지만, 실제 증권사 데이터가 같은 의미를
사용하는지는 별도로 검증해야 합니다.

## 안전 규칙

- 같은 종목이 파일에 두 번 나오면 전체 import를 거부합니다.
- 미등록 종목이나 잘못된 값이 한 행이라도 있으면 어느 행도 저장하지 않습니다.
- 전체 파일을 하나의 데이터베이스 트랜잭션으로 처리합니다.
- 저장된 값과 같은 행은 `updated_at`을 바꾸지 않습니다.
- 거래내역은 수정하지 않습니다.
- `position-reconcile`의 거래기간 순증을 현재 포지션으로 자동 사용하지 않습니다.
