# ForgetMeNot Go Implementation

Go는 ForgetMeNot의 현재 주 구현 언어다. 이 문서는 `cmd/forgetmenot` CLI의 구성,
명령군, 데이터 흐름과 개발 검증 절차를 설명한다. 제품 목표와 빠른 실행은
[README.md](README.md), 이후 작업 순서는 [PROJECT_PLAN.md](docs/PROJECT_PLAN.md)를
기준으로 한다.

## 요구 환경

- Go 1.22 이상
- Windows PowerShell 기준 명령 예시
- SQLite는 Go 드라이버를 통해 애플리케이션 내부에서 사용
- 외부 공급자 동기화 시 네트워크와 해당 API 키
- Codex 자동 분석 시 공식 Codex CLI와 ChatGPT 로그인

```powershell
go version
go run ./cmd/forgetmenot help
```

Codex처럼 사용자 프로필의 Go 캐시 쓰기가 제한된 환경에서는 프로젝트 캐시를
사용한다.

```powershell
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
```

## 저장소 구조

```text
cmd/forgetmenot/                 CLI 인자, 입출력과 의존성 조립
internal/analysis/               가격, 평가, 집중도, 시나리오와 위험 규칙
internal/alerting/               알림 후보와 중복 억제
internal/codexcli/               ChatGPT 인증 Codex 비대화형 실행
internal/dart/                   OpenDART adapter와 파서
internal/emaildelivery/          TLS SMTP 메시지와 전송
internal/investmentprofile/      투자 프로필 v2 검증
internal/krx/                    KRX 종목 마스터 adapter
internal/mirae/                  미래에셋 XLSX adapter
internal/portfolioadvice/        Codex 구조화 응답 검증과 텍스트 변환
internal/prompt/                 리서치 프롬프트 생성
internal/reporting/              이메일 보고서 구성
internal/storage/                파일 및 SQLite 저장소
internal/yahoo/                  Yahoo Finance 가격 adapter
data/                            예제 입력과 Git 제외 로컬 데이터
docs/                            기능별 데이터 계약과 운영 문서
prompts/                         반복 사용 질문 템플릿
src/, app.py, tests/             Python/Streamlit 레거시 기준 구현
```

`cmd/`에는 계산 로직을 두지 않는다. 공급자 응답은 adapter 안에서 도메인 모델로
변환하고, `internal/analysis`와 저장 계층은 특정 공급자의 JSON 구조에 의존하지
않는다.

## 명령군

전체 목록은 `go run ./cmd/forgetmenot help`로 확인한다.

### 데이터베이스와 종목

| 명령 | 역할 |
| --- | --- |
| `db-init` | SQLite schema 생성 또는 migration |
| `watchlist-sync` | 관심종목 CSV 추가·갱신 |
| `watchlist-list` | 저장 종목 조회 |
| `watchlist-delete` | 거래 이력이 없는 종목 삭제 |
| `portfolio-show` | 종목, 포지션, 거래와 가설 통합 조회 |

### 거래와 현재 포지션

| 명령 | 역할 |
| --- | --- |
| `trades-import` | 프로젝트 정규화 거래 CSV import |
| `mirae-import` | 미래에셋 거래내역 XLSX 원본 import |
| `position-reconcile` | 거래기간 순증과 저장 현재 포지션 비교 |
| `positions-import` | 확인된 현재 포지션 CSV 일괄 저장 |
| `position-set` | 단일 현재 포지션 저장 |
| `thesis-set` | 단일 투자 가설 저장 |
| `investment-profile-sync` | 프로필 전체 검증 및 가설 동기화 |

거래기간 순증은 기초잔고를 알 수 없으므로 현재 잔고로 간주하지 않는다. 실제 현재
포지션은 사용자가 확인한 `positions.csv` 또는 `position-set`으로 별도 저장한다.

### OpenDART와 KRX

| 명령 | 역할 |
| --- | --- |
| `dart-corp-sync` | 전체 기업 고유번호 동기화와 종목 매핑 |
| `krx-instrument-sync` | KRX 식별자와 증권 유형 동기화 |
| `dart-disclosure-sync` | 최근 공시 전체 페이지 수집과 저장 |
| `dart-disclosure-list` | 저장 공시 조회 |
| `dart-document-sync` | 공시 원본 ZIP 검증과 버전 저장 |
| `dart-document-list` | 공시 원본 논리 버전 조회 |
| `dart-financial-sync` | 전체 재무제표 계정 수집과 버전 저장 |
| `dart-financial-list` | 저장 재무제표와 계정 조회 |
| `dart-financial-metrics` | 표준계정 기반 핵심 지표와 비율 계산 |

재무 금액은 쉼표를 제거한 부호 포함 정수 문자열로 정규화한다. 원본 ZIP 해시와
압축 해제 내용 해시를 분리해, 재압축만 달라진 동일 문서를 새 논리 버전으로 만들지
않는다.

### 분석

| 명령 | 역할 |
| --- | --- |
| `analysis-snapshot` | 가격, 포지션, 공시와 재무의 버전 지정 입력 생성 |
| `risk-assess` | 사실, 해석, 반론과 확인 질문을 포함한 규칙 평가 |
| `research-brief` | 단일 종목 ChatGPT Plus용 프롬프트 생성 |
| `portfolio-analyze` | 통화별 가치, 손익과 집중도 계산 |
| `portfolio-scenarios` | 결정론적 하락·중립·상승 충격 계산 |
| `portfolio-brief` | 전체 포트폴리오 리서치 프롬프트 생성 |
| `analysis-run-list` | 저장된 분석 실행 이력 조회 |

`portfolio-analyze`의 기본 집중도 경계는 통화별 총노출 기준 `watch=25%`,
`high=40%`다. 시나리오는 기본적으로 모든 평가 가능 포지션에 `-20%`, `0%`,
`+20%`의 동일 충격을 적용하며 확률을 추정하지 않는다.

### 알림과 보고서

| 명령 | 역할 |
| --- | --- |
| `alert-evaluate` | 저장된 포트폴리오 분석에서 알림 후보 생성 |
| `alert-list` | 상태별 알림 후보 조회 |
| `daily-email-report` | pending 알림 보고서 미리보기 또는 전송 |
| `portfolio-response-email` | 수동으로 반입한 ChatGPT 답변 미리보기 또는 전송 |
| `portfolio-codex-email` | 가격 수집, 웹 리서치, 검증, 저장과 메일 연결 |

모든 이메일 명령은 기본적으로 미리보기다. `-send`를 명시한 경우에만 SMTP로
전송한다. `daily-email-report -test-alert -send`는 실제 알림 상태를 변경하지 않는
합성 테스트 메일을 보낸다.

## 대표 실행 순서

### 초기 데이터 구성

```powershell
go run ./cmd/forgetmenot db-init
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot mirae-import -file "C:\path\거래내역.xlsx"
go run ./cmd/forgetmenot positions-import -file data/positions.csv
go run ./cmd/forgetmenot investment-profile-sync
```

### 국내 종목 데이터 구성

```powershell
go run ./cmd/forgetmenot dart-corp-sync
go run ./cmd/forgetmenot krx-instrument-sync
go run ./cmd/forgetmenot dart-disclosure-sync -days 30
go run ./cmd/forgetmenot dart-document-sync -ticker 005930 -limit 10
go run ./cmd/forgetmenot dart-financial-sync -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
go run ./cmd/forgetmenot dart-financial-metrics -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
```

OpenDART 보고서 코드는 `11011` 사업보고서, `11012` 반기보고서, `11013` 1분기
보고서, `11014` 3분기보고서를 사용한다. 연결 재무제표는 `CFS`, 별도 재무제표는
`OFS`다.

### 포트폴리오 분석

```powershell
go run ./cmd/forgetmenot portfolio-analyze -output json
go run ./cmd/forgetmenot portfolio-scenarios -output json
go run ./cmd/forgetmenot portfolio-brief -save -output json
go run ./cmd/forgetmenot portfolio-codex-email
```

### 이메일 전송

```powershell
go run ./cmd/forgetmenot daily-email-report -test-alert -send
go run ./cmd/forgetmenot portfolio-codex-email -send
```

## 투자 프로필 v2

기본 파일은 Git에서 제외되는 `data/investment_profile.json`이다. 명령 실행 전
프로필을 파싱하고 한 트랜잭션으로 가설을 동기화한다.

주요 리밸런싱 필드는 다음과 같다.

| 필드 | 의미 |
| --- | --- |
| `mode` | 현재 `cash_flow_first`만 지원 |
| `preferred_max_realized_loss_percent` | 리밸런싱 매도에서 선호하는 손실률 절댓값 |
| `hard_max_realized_loss_percent` | 가설이 유지될 때 넘을 수 없는 손실률 절댓값 |
| `thesis_invalidation_overrides_limit` | 가설 훼손 시 최대 손실률 예외 허용 여부 |
| `max_turnover_percent` | 허용 가능한 회전율 상한 |
| `target_horizon_months` | 목표 배분에 접근할 기간 |
| `force_target_allocation_by_deadline` | 기한 내 강제 조정 여부 |

`portfolio-codex-email`은 부분·전량 매도 의견의 종목별 평균단가 수익률을 확인한다.
가설이 `broken`이 아닌데 예상 손실이 최대 한도를 넘으면 구조화 응답 검증에 실패하고
파일과 이메일 생성을 중단한다. 평균단가가 없어 손실률을 계산할 수 없는 경우에도
매도 권고를 통과시키지 않는다.

## Codex 분석 경계

`portfolio-codex-email`은 다음 순서로 동작한다.

1. 투자 프로필 검증과 가설 동기화
2. Yahoo 가격 수집과 포트폴리오 평가
3. 분석 입력과 프롬프트의 SQLite 저장
4. 빈 임시 작업 디렉터리에서 Codex CLI 실행
5. ChatGPT 로그인 상태와 실시간 웹 검색 사용
6. JSON Schema, 활성 종목, 수량, 보호 수량, 출처와 손실 한도 검증
7. 검증된 응답만 일반 텍스트로 변환하고 로컬 파일에 저장
8. `-send`가 있을 때만 SMTP 전송

Codex 자식 프로세스에서는 `OPENAI_API_KEY`와 `CODEX_API_KEY`를 제거한다. 분석은
공식 OpenDART, 회사 IR, SEC, ETF 운용사와 공식 거시자료를 우선하지만, 인용문이
원문과 완전히 일치하는지는 아직 별도 코드로 재검증하지 않는다.

## 환경변수

| 변수 | 사용처 |
| --- | --- |
| `OPENDART_API_KEY` | OpenDART 기업, 공시, 원본과 재무제표 API |
| `KRX_API_KEY` | KRX Open API 종목 마스터 |
| `OPENAI_API_KEY` | 미래 연동용 예약값, 현재 Go CLI에서는 사용하지 않음 |
| `SMTP_HOST`, `SMTP_PORT` | SMTP 서버 |
| `SMTP_USERNAME`, `SMTP_PASSWORD` | SMTP 인증 |
| `SMTP_TLS_MODE` | `starttls` 또는 `implicit` |
| `EMAIL_FROM`, `EMAIL_TO` | 발신자와 쉼표 구분 수신자 |
| `EMAIL_SUBJECT_PREFIX` | 제목 접두사 |
| `EMAIL_REPORT_TIMEZONE` | 보고서 표시 시간대 |

평문 SMTP는 거부한다. Gmail처럼 2단계 인증을 사용하는 공급자는 일반 비밀번호가
아니라 해당 공급자가 발급한 앱 비밀번호가 필요할 수 있다.

## 데이터 상태와 오류 처리

공급자 결과는 다음 상태를 사용한다.

| 상태 | 의미 |
| --- | --- |
| `available` | 필요한 데이터가 정상 수집됨 |
| `partial` | 일부 데이터만 유효함 |
| `empty` | 요청은 성공했지만 결과가 없음 |
| `unavailable` | 네트워크 또는 공급자 오류 |
| `not_requested` | 키, 기업코드 또는 대상 조건이 없어 요청하지 않음 |

외부 요청에는 timeout과 제한된 재시도를 적용한다. 한 기업이나 데이터셋의 실패가
성공한 다른 결과까지 불필요하게 폐기하지 않게 하며, 빈 전체 스냅샷으로 기존
마스터를 지우지 않는다.

## 저장과 정밀도

- 기본 DB는 `data/forgetmenot.db`다.
- 금액과 수량은 소수점 8자리 고정 정밀도 정수로 저장한다.
- 큰 재무 금액은 부호 포함 정수 문자열로 보존한다.
- 공시, 재무와 분석 실행은 내용 또는 입력·규칙·출력 해시를 기록한다.
- 실제 DB, CSV, 원본 자료, cache, 투자 프로필과 보고서는 Git에서 제외한다.
- import와 동기화는 같은 입력의 반복 실행에서 중복을 만들지 않게 설계한다.

## 테스트와 검증

Go 파일을 수정한 뒤 다음 명령을 모두 실행한다.

```powershell
gofmt -w cmd internal
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
go build ./...
go test ./...
go vet ./...
git diff --check
```

Yahoo, OpenDART와 KRX adapter의 단위 테스트는 `internal/*/testdata` fixture를
사용하며 실제 네트워크를 호출하지 않는다. 실제 API smoke test는 별도 운영
검증으로 취급한다.

## 상세 문서

- [PROJECT_PLAN.md](docs/PROJECT_PLAN.md): 단계별 개발 로드맵
- [TRADE_CSV.md](docs/TRADE_CSV.md): 거래 import 계약
- [POSITION_CSV.md](docs/POSITION_CSV.md): 현재 포지션 import 계약
- [PORTFOLIO_VALUATION.md](docs/PORTFOLIO_VALUATION.md): 포트폴리오 계산 계약
- [PORTFOLIO_SCENARIOS.md](docs/PORTFOLIO_SCENARIOS.md): 스트레스 테스트 계약
- [PRICE_METRICS.md](docs/PRICE_METRICS.md): 가격 지표 계산 계약
- [ANALYSIS_RUNS.md](docs/ANALYSIS_RUNS.md): 분석 버전과 이력
- [ALERTS.md](docs/ALERTS.md): 알림 후보와 상태 전이
- [EMAIL_REPORTS.md](docs/EMAIL_REPORTS.md): SMTP 운영 절차
- [INVESTMENT_PROFILE.md](docs/INVESTMENT_PROFILE.md): 투자 프로필 v2
- [PORTFOLIO_RESPONSE_EMAIL.md](docs/PORTFOLIO_RESPONSE_EMAIL.md): Codex 분석 이메일

## 미구현 범위

- 상시 실행 스케줄러와 quiet hours
- 이메일 전송 이력 테이블
- 읽기 전용 HTTP API와 인증·rate limit·감사 로그
- ChatGPT Custom GPT Actions
- 전용 웹 또는 모바일 UI
- 자동 주문
- SEC, FRED와 뉴스 자료의 장기 구조화 저장 및 시점 간 비교

Python/Streamlit 코드는 포팅 비교를 위한 레거시 기준 구현이다. 신규 기능은 별도
결정이 없으면 Go에만 추가한다.
