# ForgetMeNot: Stock Discussion Bot

ForgetMeNot is a personal stock research and portfolio review bot that combines
market prices, disclosures, financial statements, investment theses, and live
web research.

현재 주 구현체는 Go CLI로, Yahoo Finance, OpenDART, KRX, 미래에셋 거래내역과
사용자가 확인한 현재 포지션을 SQLite에 저장하고, 계산 가능한 지표는 Go에서
결정론적으로 계산합니다. ChatGPT 계정으로 인증된 Codex CLI는 최신 공개자료 조사와
해석을 담당하며, 결과를 일반 텍스트 이메일로 보낼 수 있습니다..

이 프로젝트는 개인용 투자 리서치 도구이며 자동 주문이나 수익 보장형 추천은
구현하지 않습니다.

## 핵심 기능

- 관심종목과 현재 포지션, 거래내역, 투자 가설의 SQLite 저장
- 미래에셋 `거래내역.xlsx`와 정규화 거래 CSV의 멱등 import
- Yahoo Finance 가격, 수익률, 이동평균, 변동성, 최대낙폭과 거래량 지표
- KRX 공식 종목 식별자와 보통주, 우선주, ETF, ETN 분류
- OpenDART 기업코드, 공시 목록, 공시 원본과 전체 재무제표 수집
- 공시 원본과 재무제표의 내용 해시 기반 논리 버전 관리
- 통화별 포트폴리오 평가, 미실현손익, 집중도와 스트레스 시나리오
- 입력, 규칙, 출력 해시를 보존하는 분석 실행 이력
- 집중도와 데이터 품질 알림의 중복 억제 및 이메일 보고서
- 투자 프로필에 정의된 목표 배분, 보호 수량, 증액 조건과 리밸런싱 정책
- ChatGPT 로그인 기반 Codex CLI의 실시간 웹 리서치와 포트폴리오 분석 이메일

## 현재 분석 흐름

```text
거래내역 / 현재 포지션 / 관심종목
  -> SQLite 저장과 종목 식별자 검증
  -> Yahoo 가격 + OpenDART 공시·재무 + KRX 분류
  -> 포트폴리오 평가와 가격 지표·시나리오 계산
  -> 투자 프로필과 종목별 가설 결합
  -> Codex CLI 실시간 공개자료 조사
  -> 구조화 응답과 매도 손실 한도 검증
  -> 일반 텍스트 보고서 저장
  -> 사용자가 `-send`를 지정한 경우에만 이메일 전송
```

Go가 가격, 손익, 비중, 시나리오와 임계값을 계산하고, Codex는 공식 공시, 회사 IR,
SEC, ETF 운용사와 공식 거시자료를 우선 조사해 사실, 해석, 반론과 행동 의견을
작성합니다.

## 빠른 시작

### 1. 준비

- Go 1.22 이상
- Windows PowerShell
- OpenDART 또는 KRX 동기화 시 해당 서비스의 API 키
- 자동 포트폴리오 리서치 시 ChatGPT로 로그인한 공식 Codex CLI
- 이메일 전송 시 TLS를 지원하는 SMTP 계정

```powershell
go version
Copy-Item .env.example .env
go run ./cmd/forgetmenot db-init
```

`.env`에는 필요한 키와 SMTP 정보만 로컬에서 입력합니다. `.env`, 실제 DB, 실제
포지션과 거래 CSV, 원본 공시 및 생성 보고서는 Git에서 제외됩니다.

### 2. 기본 데이터 저장

```powershell
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot positions-import -file data/positions.csv
go run ./cmd/forgetmenot investment-profile-sync
```

미래에셋 원본 거래내역을 가져올 때는 다음 명령을 사용합니다.

```powershell
go run ./cmd/forgetmenot mirae-import -file "C:\path\거래내역.xlsx"
```

### 3. 공급자 데이터 동기화

```powershell
go run ./cmd/forgetmenot dart-corp-sync
go run ./cmd/forgetmenot krx-instrument-sync
go run ./cmd/forgetmenot dart-disclosure-sync -days 30
go run ./cmd/forgetmenot dart-document-sync -ticker 005930 -limit 10
go run ./cmd/forgetmenot dart-financial-sync -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
```

KRX Open API는 실시간 시세 API가 아닙니다. 종목 식별과 분류에는 KRX를 사용하고,
현재 가격과 가격 이력은 Yahoo Finance에서 가져옵니다.

### 4. 포트폴리오 확인

```powershell
go run ./cmd/forgetmenot portfolio-analyze -output text
go run ./cmd/forgetmenot portfolio-scenarios -output text
go run ./cmd/forgetmenot portfolio-brief -output text
```

포트폴리오는 KRW와 USD를 검증된 환율 없이 임의 합산하지 않습니다. 집중도와 목표
배분도 기본적으로 각 통화의 총 노출 안에서 해석합니다.

### 5. Codex 분석과 이메일

```powershell
codex login
codex login status
go run ./cmd/forgetmenot portfolio-codex-email
```

기본 실행은 최신 가격과 웹 리서치를 사용해 보고서를 만들고 미리보기만 수행합니다.
내용을 확인한 뒤 실제 이메일을 보낼 때만 `-send`를 붙입니다.

```powershell
go run ./cmd/forgetmenot portfolio-codex-email -send
```

이 경로는 OpenAI Platform API를 직접 호출하지 않습니다. 자식 Codex 프로세스에는
`OPENAI_API_KEY`와 `CODEX_API_KEY`를 전달하지 않고, 로그인한 ChatGPT 플랜의
Codex 사용량 한도를 사용합니다.

## 투자 프로필과 리밸런싱

실제 정책은 Git에서 제외되는 `data/investment_profile.json`에 저장합니다. 공개 형식은
`data/investment_profile.example.json`을 참고합니다.

현재 프로필 v2는 다음을 표현할 수 있습니다.

- 코어, 성장, 방어 자산의 목표 비중
- 종목별 매수 가설, 무효화 조건, 증액 조건과 확인 지표
- 매도에서 제외할 보호 수량
- 기존 자산 매도보다 신규 자금을 먼저 쓰는 `cash_flow_first`
- 리밸런싱 매도의 선호 손실률과 최대 손실률
- 투자 가설 훼손 시 손실 한도 예외
- 최대 회전율, 목표 접근 기간과 기한 내 강제 조정 여부

가설이 유지되는 종목의 매도 권고가 프로필의 최대 실현손실 한도를 넘으면 분석
응답을 거부하고 이메일도 보내지 않습니다. 이 한도는 자동 손절선이 아니라,
리밸런싱만을 이유로 큰 손실을 확정하는 것을 막는 검증 규칙입니다.

## 데이터와 보안 원칙

- 증권사 로그인 화면, 인증서와 비밀번호를 자동화하거나 스크래핑하지 않습니다.
- API 키, SMTP 비밀번호, 계좌 원본과 실제 포트폴리오는 커밋하지 않습니다.
- 외부 데이터에는 공급자, 출처 URL, 관측시각과 수집시각을 기록합니다.
- 공급자 일부 실패를 정상값으로 바꾸지 않고 `partial`, `unavailable` 등으로 남깁니다.
- 금액과 수량은 필요한 정밀도를 보존하고 중요한 재무 금액은 `float64`로 만들지 않습니다.
- 같은 입력의 반복 import와 동기화는 중복 데이터를 만들지 않아야 합니다.
- 이메일 전송은 항상 명시적인 `-send`가 있을 때만 수행합니다.

## 비목표와 현재 한계

- 자동 주문과 증권사 계정 제어
- 수익률 보장 또는 모델 답변의 무조건적 실행
- 검증되지 않은 환율을 이용한 KRW·USD 총자산 합산
- 상시 실행 스케줄러, quiet hours와 전송 이력 관리
- 전용 웹 또는 모바일 UI
- SEC, FRED와 뉴스의 장기 구조화 저장 및 변화 추적

## 프로젝트 문서

`docs/`의 파일들은 임시 메모가 아니라 기능별 계약과 운영 절차입니다. README에는
처음 실행할 때 필요한 핵심만 두고, 계산식, 데이터 형식, 실패 처리와 보안 규칙은
아래 문서를 단일 기준으로 사용합니다.

| 문서 | 역할 |
| --- | --- |
| [PROJECT_PLAN](docs/PROJECT_PLAN.md) | 제품 원칙, 확정된 기술 결정, 단계별 로드맵과 완료 기준 |
| [TRADE_CSV](docs/TRADE_CSV.md) | 정규화 거래 형식, 중복 방지와 미래에셋 원본 매핑 |
| [POSITION_CSV](docs/POSITION_CSV.md) | 확인된 현재 포지션 CSV 형식과 원자적 import 규칙 |
| [PORTFOLIO_VALUATION](docs/PORTFOLIO_VALUATION.md) | 평가금액, 손익, 집중도와 통화 처리 계약 |
| [PORTFOLIO_SCENARIOS](docs/PORTFOLIO_SCENARIOS.md) | 하락·중립·상승 스트레스 테스트 계산과 한계 |
| [PRICE_METRICS](docs/PRICE_METRICS.md) | 수익률, 변동성, 낙폭, 거래량 지표의 계산식과 임계값 |
| [ANALYSIS_RUNS](docs/ANALYSIS_RUNS.md) | 분석 입력·규칙·출력 해시와 SQLite 실행 이력 |
| [ALERTS](docs/ALERTS.md) | 알림 후보 규칙, fingerprint와 중복 억제 상태 전이 |
| [EMAIL_REPORTS](docs/EMAIL_REPORTS.md) | SMTP 설정, 테스트 메일, 미리보기와 전송 규칙 |
| [INVESTMENT_PROFILE](docs/INVESTMENT_PROFILE.md) | 프로필 v2 형식, 투자 가설과 리밸런싱 정책 |
| [PORTFOLIO_RESPONSE_EMAIL](docs/PORTFOLIO_RESPONSE_EMAIL.md) | Codex 인증, 웹 리서치, 응답 검증과 이메일 흐름 |

개발자를 위한 Go 패키지 구조와 전체 CLI 명령군은 [README_GO.md](README_GO.md),
저장소 작업·보안·검증 규칙은 [AGENTS.md](AGENTS.md)를 참고합니다.

## 검증

```powershell
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
go build ./...
go test ./...
go vet ./...
git diff --check
```

Yahoo, OpenDART와 KRX 파서 단위 테스트는 저장된 fixture를 사용하며 실제 네트워크를
호출하지 않습니다.

## Python 레거시

`app.py`, `src/stock_research_bot`, `tests`는 초기 Python/Streamlit 프로토타입입니다.
Go 포팅 결과를 비교하는 기준 구현으로 남겨 두었으며, 별도 결정이 없으면 신규
기능은 Go에만 추가합니다.
