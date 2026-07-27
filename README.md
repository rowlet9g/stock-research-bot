# FORGETMENOT: Stock Discussion Bot

DART 공시/재무 정보와 Yahoo Finance 시세 데이터를 결합해 투자 공부용 리서치 봇을 만드는 프로젝트입니다.

## 프로젝트 문서

- [프로젝트 계획](docs/PROJECT_PLAN.md): 목표 아키텍처, 데이터 정책, 단계별 로드맵과 완료 기준
- [거래 CSV 계약](docs/TRADE_CSV.md): 정규화 거래 형식, 중복 방지 규칙, 미래에셋 원본 매핑 상태
- [저장소 작업 지침](AGENTS.md): 구현, 보안, 테스트, 검증 및 Git 규칙
- [Go 포팅 현황](README_GO.md): 현재 Go CLI 범위와 실행 방법

## 목표

- 관심종목의 가격, 거래량, 추세를 요약한다.
- DART 공시를 수집하고 투자 관점에서 요약한다.
- 매수/매도 이유를 투자 일지로 기록한다.
- AI가 확정적 매수/매도 지시가 아니라 근거, 반론, 확인할 지표를 제시한다.

## 비목표

- 자동 주문
- 수익 보장형 추천
- 비공식 증권사 로그인/스크래핑

## 현재 구현 상태

초기 Python/Streamlit 프로토타입은 남겨두되, 현재 주 개발 방향은 Go 포트입니다.

Go 포트는 `cmd/forgetmenot` CLI에서 아래 흐름을 먼저 구현합니다.

- 관심종목 CSV 로드
- Yahoo Finance 가격 스냅샷 생성
- 가격 신호 규칙 평가
- OpenDART 최근 공시 수집
- ChatGPT Plus에 붙여넣을 투자 브리핑 프롬프트 생성
- 데이터 출처, 기준시각, 수집시각 기록
- 관심종목 CSV 헤더와 행 검증
- 사람용 text 출력과 프로그램용 JSON 출력
- SQLite migration과 투자 기록 영구 저장
- 관심종목, 포지션, 거래, 투자 가설 통합 조회
- 중복 실행에 안전한 정규화 거래 CSV import
- 미래에셋 거래내역 XLSX 원본 import와 종목 alias cache
- KRX 공식 종목 마스터 동기화와 보통주, 우선주, ETF, ETN 분류
- KRX 단축코드, 표준코드와 OpenDART 기업코드 교차 검증

## 개발 환경 준비

Go 1.22 이상과 PowerShell을 기준으로 합니다.

```powershell
go version
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
go build ./...
go test ./...
go vet ./...
```

일반 로컬 터미널에서는 Go 기본 캐시를 사용해도 됩니다. 위의 프로젝트 전용 캐시는
Codex처럼 사용자 프로필의 Go 캐시 쓰기가 제한된 환경에서도 같은 검증 명령을
재현하기 위한 설정입니다.

## Go 실행

```powershell
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자 -thesis "실적 턴어라운드 기대" -output text
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name Apple -output json
```

이 명령은 Yahoo Finance에 실제 네트워크 요청을 보냅니다. Yahoo 응답이 실패해도
CLI는 가격을 `N/A`로 표시하고 프롬프트를 생성합니다.

OpenDART를 사용하려면 `.env.example`을 `.env`로 복사한 뒤
`OPENDART_API_KEY`를 설정합니다. KRX 종목 마스터를 사용하려면 같은 파일에
`KRX_API_KEY`를 설정합니다. API 키는 로그, 출처 URL, Git에 기록하지 않습니다.

`OPENAI_API_KEY` 자동 호출은 아직 Go 포트에 넣지 않았습니다. Plus 요금제 안에서 쓰는 흐름은 앱이 프롬프트를 생성하고 사용자가 ChatGPT에 붙여넣는 방식으로 둡니다.

## SQLite와 투자 기록

기본 데이터베이스는 `data/forgetmenot.db`입니다. 다음 순서로 관심종목과 투자
기록을 저장할 수 있습니다.

```powershell
go run ./cmd/forgetmenot db-init
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot dart-corp-sync
go run ./cmd/forgetmenot krx-instrument-sync
go run ./cmd/forgetmenot position-set -ticker AAPL -quantity 2 -average-cost 210.50 -currency USD -as-of 2026-07-23
go run ./cmd/forgetmenot thesis-set -ticker AAPL -summary "서비스 매출 성장" -invalidation "서비스 성장률 둔화" -horizon "12개월" -metrics "서비스 매출,마진"
go run ./cmd/forgetmenot trades-import -file data/trades.normalized.example.csv -source mirae-normalized
go run ./cmd/forgetmenot mirae-import -file "C:\path\거래내역.xlsx"
go run ./cmd/forgetmenot portfolio-show -ticker AAPL -output json
```

`watchlist-sync`는 CSV 종목을 추가하거나 갱신합니다. 저장된 거래가 없는 종목은
`watchlist-delete -ticker <ticker>`로 삭제할 수 있습니다.

`dart-corp-sync`는 OpenDART의 기업 고유번호 ZIP/XML 전체 파일을 받아
`dart_corporations`에 원자적으로 갱신합니다. 원본이 비었거나, 중복 식별자나 잘못된
행이 있거나, 다운로드가 중단되면 기존 현재 목록과 종목 매핑은 유지됩니다. 정상
목록에 없는 과거 기업은 삭제하지 않고 비활성 상태로 보존합니다.

국내 원화 종목은 6자리 대문자 영숫자 종목코드가 정확히 일치할 때
`instruments.dart_corp_code`가 갱신됩니다. 이후 분석 명령은 관심종목 CSV의
`dart_corp_code`가 비어 있으면 SQLite에 저장된 값을 사용합니다. DART 파일은
기업별 대표 종목코드를 제공하므로 ETF, ETN, 우선주 등 모든 국내 증권을 완전히
매핑하지는 못하며 이 범위는 KRX 식별자 동기화에서 보완합니다.

OpenDART 요청은 30초 HTTP timeout, 최대 3회 시도, CLI 전체 2분 timeout을
적용합니다. HTTP 429와 5xx만 제한적으로 재시도합니다. 출처 URL에는 인증키를
기록하지 않으며 각 기업 행에 원본 변경일과 수집시각을 저장합니다. 현재 OpenDART
서버와 Go의 TLS 호환을 위해 전용 client에서 TLS 1.2 이상과 AES-GCM 기반 RSA
키 교환 fallback을 허용합니다.

`krx-instrument-sync`는 KRX Open API의 아래 다섯 서비스를 데이터셋별로
동기화합니다. KRX Data Marketplace에서 인증키를 발급받고 각 서비스를 신청해
승인받아야 합니다.

- 유가증권 종목기본정보
- 코스닥 종목기본정보
- 코넥스 종목기본정보
- ETF 일별매매정보
- ETN 일별매매정보

`-date YYYY-MM-DD`를 생략하면 서울 시간 기준 직전 평일부터 KOSPI 데이터가 있는
최근 날짜를 최대 10개 평일까지 역으로 찾습니다. 날짜를 지정하면 그 날짜만
요청합니다.

KRX의 6자리 단축코드가 저장된 원화 종목 ticker와 정확히 일치할 때
`krx_standard_code`, `instrument_type`, `krx_verified_at`을 갱신합니다. 보통주만
동일한 6자리 OpenDART 종목코드와 교차 매핑하며 우선주, ETF, ETN을 기업코드로
추측하지 않습니다. 새 전체 스냅샷에서 사라진 식별자는 과거 KRX 행을 보존하되
현재 종목의 KRX 검증 표시를 해제합니다.

다섯 데이터셋 중 일부 요청이 실패하면 성공한 데이터셋만 저장하고 결과를
`partial`로 표시합니다. 빈 응답은 정상 전체 스냅샷으로 간주하지 않으므로 기존
마스터를 지우지 않습니다. KRX 서비스는 종목 식별과 일별 정보용이며 실시간 시세
API가 아닙니다.

2026-07-27 확인 기준 KRX Open API 인증키 유효기간은 1년이고 호출 한도는 일
10,000회입니다. 비상업적 용도로만 사용할 수 있고 제3자 재배포가 제한되며, KRX
통계정보를 이용했다는 사실을 표시해야 합니다. 운영 전에는 최신
[서비스 목록](https://openapi.krx.co.kr/contents/OPP/INFO/service/OPPINFO004.cmd),
[이용방법](https://openapi.krx.co.kr/contents/OPP/INFO/OPPINFO003.jsp),
[이용약관](https://openapi.krx.co.kr/contents/OPP/INFO/OPPINFO002.jsp)을 다시
확인합니다.

금액과 수량은 SQLite에 소수점 8자리 고정 정밀도 정수로 저장합니다. DB 파일,
개인 거래 CSV, 계좌 원본 CSV는 Git에서 제외됩니다.

`data/trades.normalized.example.csv`는 프로젝트가 정의한 중간 형식이며 미래에셋
원본 내보내기 형식을 의미하지 않습니다.

`mirae-import`는 미래에셋에서 내보낸 `거래내역` XLSX를 직접 읽습니다. 거래
원장은 주식 입출고와 현금 입출금을 분리하므로 수량이 기록된 아래 네 거래종류만
체결로 가져옵니다.

- `주식매수입고`
- `주식매도출고`
- `해외주식매수입고`
- `해외주식매도출고`

단가는 거래금액 또는 외화거래금액을 수량으로 나눠 계산합니다. 체결시각은 만들지
않고 날짜 정밀도로 저장하며, 원본에 없는 개별 거래세는 `taxes_known=false`로
구분합니다.

종목명 매핑은 저장된 종목, 검증된 alias cache, Yahoo 검색 순서로 해결합니다.
개인 계좌 종목이 들어가는 기본 cache는
`data/cache/mirae_instrument_aliases.csv`이며 Git에서 제외됩니다. 공개 형식은
`data/mirae_instrument_aliases.example.csv`를 참고합니다.

### 데이터 상태

가격과 공시 결과의 `status`는 다음 값을 사용합니다.

| 상태 | 의미 |
| --- | --- |
| `available` | 필요한 데이터가 정상적으로 수집됨 |
| `partial` | 일부 값이 없거나 응답 일부가 유효하지 않음 |
| `empty` | 요청은 성공했지만 조회된 데이터가 없음 |
| `unavailable` | 네트워크 또는 공급자 오류로 수집하지 못함 |
| `not_requested` | API 키나 기업코드가 없어 요청하지 않음 |

JSON 출력의 `issues`에는 `invalid_request`, `unavailable`, `bad_response`,
`no_data`, `partial_data`, `not_requested`와 같은 원인 분류가 포함됩니다.

## Python 레거시 프로토타입

Python/Streamlit 코드는 Go 포팅 결과를 비교하기 위한 기준 구현입니다. 별도 결정이
없으면 신규 기능은 Go에만 추가합니다.

```powershell
python -m venv .venv
.\.venv\Scripts\Activate.ps1
python -m pip install -e ".[dev]"
Copy-Item .env.example .env
streamlit run app.py
```

## 데이터 입력

관심종목은 `data/watchlist.example.csv` 형식을 기준으로 관리합니다.

```csv
name,ticker,yahoo_ticker,dart_corp_code,market,currency
삼성전자,005930,005930.KS,,KOSPI,KRW
Apple,AAPL,AAPL,,NASDAQ,USD
```

`dart_corp_code`는 OpenDART의 고유번호입니다. 값은 비워 둬도 되며,
`watchlist-sync` 후 `dart-corp-sync`를 실행하면 가능한 국내 종목이 자동
매핑됩니다. 이어서 `krx-instrument-sync`를 실행하면 국내 증권의 공식 KRX
식별자와 유형이 보완됩니다.

CSV는 `name`, `ticker`, `yahoo_ticker`, `dart_corp_code`, `market`,
`currency` 헤더를 모두 포함해야 합니다. `dart_corp_code` 값은 비워둘 수 있지만
나머지 값은 각 행에 필수입니다.

## 추천 원칙

AI 출력은 항상 아래처럼 분리합니다.

- 사실: 가격, 공시 제목, 재무 수치처럼 데이터로 확인되는 내용
- 해석: 수치와 이벤트가 의미할 수 있는 가능성
- 확인 질문: 투자 가설이 아직 유효한지 검증하는 질문
