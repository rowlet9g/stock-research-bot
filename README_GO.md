# ForgetMeNot Go Port

이 문서는 Python MVP를 Go로 옮기는 초기 포팅 상태를 설명합니다.
현재 개발 및 검증 절차는 루트 `README.md`, 이후 작업 순서는
`docs/PROJECT_PLAN.md`를 기준으로 합니다.

## 현재 범위

- 관심종목 CSV 로드
- Yahoo Finance chart endpoint 기반 가격 스냅샷 생성
- 1일 변동률, 20일/60일 이동평균, 거래량 계산
- 가격 신호 규칙 평가
- OpenDART 최근 공시 수집
- ChatGPT Plus에 붙여넣을 브리핑 프롬프트 생성
- 가격 bar의 날짜와 거래량 정렬
- 데이터 상태와 출처 메타데이터 기록
- 관심종목 CSV 검증
- text/JSON 출력
- SQLite migration과 repository
- 관심종목, 포지션, 거래, 투자 가설 저장
- 정규화 거래 CSV의 중복 없는 import
- 미래에셋 거래내역 XLSX 원본 import
- 거래 단가 파생, 데이터 품질 상태, 결정론적 거래 ID
- 검증된 종목 alias cache와 Yahoo 종목 검색
- OpenDART 기업 고유번호 전체 목록 동기화
- SQLite 국내 종목의 DART 고유번호 자동 매핑
- KRX 종목 마스터 전체 동기화
- KRX 단축코드, 표준코드와 보통주, 우선주, ETF, ETN 유형 저장
- KRX와 OpenDART 식별자 교차 검증

OpenAI API 자동 호출은 아직 Go 포트에 넣지 않았습니다. Plus 요금제 안에서 쓰려면 앱이 프롬프트를 생성하고 사용자가 ChatGPT에 붙여넣는 방식이 추가 과금 없이 가장 안전합니다.

## 실행

Go 1.22 이상이 필요합니다.

```powershell
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자 -thesis "실적 턴어라운드 기대" -output text
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name Apple -output json
```

DART 공시까지 보려면 `.env` 또는 환경변수에 `OPENDART_API_KEY`를 설정합니다.
관심종목을 SQLite에 저장한 뒤 기업 고유번호를 동기화하면 CSV의
`dart_corp_code`를 직접 채우지 않아도 분석 명령이 저장된 매핑을 사용합니다.

```powershell
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot dart-corp-sync
go run ./cmd/forgetmenot krx-instrument-sync
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자
```

KRX 동기화에는 `.env` 또는 환경변수의 `KRX_API_KEY`와 KRX Data
Marketplace의 유가증권, 코스닥, 코넥스 종목기본정보 및 ETF, ETN 일별매매정보
서비스 승인이 필요합니다. 운영 키가 없는 개발 환경에서는 fixture 테스트만
실행되며 실제 API smoke test는 수행되지 않습니다.

SQLite 및 투자 기록 명령은 루트 `README.md`와 `docs/TRADE_CSV.md`를
참고합니다.

## 테스트

```powershell
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
go build ./...
go test ./...
go vet ./...
```

Yahoo, OpenDART, KRX 파서는 `internal/*/testdata` fixture를 사용해 실제
네트워크 없이 검증합니다.
