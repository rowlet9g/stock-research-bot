# ForgetMeNot: Stock Discussion Bot

DART 공시/재무 정보와 Yahoo Finance 시세 데이터를 결합해 투자 공부용 리서치 봇을 만드는 프로젝트입니다.

## 프로젝트 문서

- [프로젝트 계획](docs/PROJECT_PLAN.md): 목표 아키텍처, 데이터 정책, 단계별 로드맵과 완료 기준
- [거래 CSV 계약](docs/TRADE_CSV.md): 정규화 거래 형식, 중복 방지 규칙, 미래에셋 원본 매핑 상태
- [현재 포지션 CSV](docs/POSITION_CSV.md): 확인된 잔고 스냅샷의 일괄 입력 형식과 안전 규칙
- [포트폴리오 평가](docs/PORTFOLIO_VALUATION.md): 통화별 가치, 손익과 집중도 계산 계약
- [포트폴리오 시나리오](docs/PORTFOLIO_SCENARIOS.md): 확률을 만들지 않는 결정론적 스트레스 테스트
- [가격 지표](docs/PRICE_METRICS.md): 수익률, 변동성, 낙폭과 거래량 신호 계산 계약
- [분석 실행 이력](docs/ANALYSIS_RUNS.md): 입력·규칙·출력 해시 기반 SQLite 이력
- [알림 후보](docs/ALERTS.md): 포트폴리오 사건 판정, 중복 억제와 이메일 상태 전이
- [일간 이메일 보고서](docs/EMAIL_REPORTS.md): SMTP 설정, 미리보기, 전송과 운영 한계
- [Codex 포트폴리오 분석 이메일](docs/PORTFOLIO_RESPONSE_EMAIL.md): ChatGPT 로그인 기반 자동 분석과 전송
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
- OpenDART 공시 목록 전체 페이지 수집과 SQLite 영구 저장
- 공시 접수번호 기반 중복 방지와 저장된 공시 조회
- OpenDART 공시 원본 ZIP 검증, 파일 저장과 논리 버전 추적
- OpenDART 전체 재무제표 계정 정규화, SQLite 버전 저장과 조회
- OpenDART 표준계정 기반 핵심 재무지표 매핑과 재무비율 계산
- 가격, 포트폴리오, 공시와 재무정보의 버전 지정 분석 입력 스냅샷
- 사실, 가능한 해석, 확인 질문과 출처 증거를 분리한 위험 규칙 평가
- 통합 입력과 위험 평가를 포함하는 ChatGPT Plus용 리서치 브리핑 생성
- 포트폴리오 평가·시나리오를 포함하는 ChatGPT Plus용 전체 브리핑 생성
- 거래기간 수량 순증과 저장된 현재 포지션의 읽기 전용 대조
- 확인된 현재 포지션 CSV의 원자적이고 멱등한 일괄 저장
- 통화별 포지션 가치, 미실현손익과 총 노출액 기준 집중도 분석
- 입력 평가 해시와 한계를 포함하는 하락·중립·상승 시나리오 분석
- 20·60일 수익률과 연율화 변동성, 6개월 최대 낙폭 및 거래량 배수
- 실측값, 임계값과 계산식을 포함하는 가격 위험 신호
- 입력·규칙·출력 해시를 보존하는 분석 실행 이력
- 집중도와 데이터 품질 알림 후보의 결정론적 생성 및 중복 억제 저장
- TLS SMTP 기반 일간 알림 보고서 미리보기와 명시적 이메일 전송
- ChatGPT Plus 답변 파일과 분석 실행 해시를 연결한 상세 분석 이메일 전송
- ChatGPT 로그인 상태의 Codex CLI를 이용한 포트폴리오 분석·저장·메일 자동 연결

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

`OPENAI_API_KEY`를 사용하는 Platform API 자동 호출은 Go 포트에 넣지 않았습니다.
대신 `portfolio-codex-email`이 ChatGPT로 로그인된 공식 Codex CLI를 비대화형으로
실행합니다. 이 명령은 API 키 환경변수를 Codex 자식 프로세스에 전달하지 않으며
로그인한 ChatGPT 플랜의 Codex 사용량 한도를 사용합니다.

## SQLite와 투자 기록

기본 데이터베이스는 `data/forgetmenot.db`입니다. 다음 순서로 관심종목과 투자
기록을 저장할 수 있습니다.

```powershell
go run ./cmd/forgetmenot db-init
go run ./cmd/forgetmenot watchlist-sync -watchlist data/watchlist.example.csv
go run ./cmd/forgetmenot dart-corp-sync
go run ./cmd/forgetmenot krx-instrument-sync
go run ./cmd/forgetmenot dart-disclosure-sync -days 30
go run ./cmd/forgetmenot dart-disclosure-list -ticker 005930 -limit 20
go run ./cmd/forgetmenot dart-document-sync -ticker 005930 -limit 10
go run ./cmd/forgetmenot dart-document-list -receipt-no 20260727000099
go run ./cmd/forgetmenot dart-financial-sync -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
go run ./cmd/forgetmenot dart-financial-list -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS -account-limit 100
go run ./cmd/forgetmenot dart-financial-metrics -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
go run ./cmd/forgetmenot analysis-snapshot -ticker 005930 -output json
go run ./cmd/forgetmenot risk-assess -ticker 005930 -output json
go run ./cmd/forgetmenot research-brief -ticker 005930 -question "현재 투자 가설에서 가장 먼저 확인할 위험은?" -output text
go run ./cmd/forgetmenot position-reconcile -output json
go run ./cmd/forgetmenot positions-import -file data/positions.csv
go run ./cmd/forgetmenot portfolio-analyze -output json
go run ./cmd/forgetmenot portfolio-scenarios -downside-bps -2000 -upside-bps 2000 -output json
go run ./cmd/forgetmenot portfolio-brief -output text
go run ./cmd/forgetmenot portfolio-brief -question "가장 먼저 확인할 집중 위험은?" -output text
go run ./cmd/forgetmenot portfolio-brief -question-file prompts/portfolio_review.md -output text
go run ./cmd/forgetmenot portfolio-brief -save -output json
go run ./cmd/forgetmenot analysis-run-list -kind portfolio_brief
go run ./cmd/forgetmenot alert-evaluate -run-id 1
go run ./cmd/forgetmenot alert-list -status pending
go run ./cmd/forgetmenot daily-email-report
go run ./cmd/forgetmenot daily-email-report -test-alert -send
go run ./cmd/forgetmenot daily-email-report -send
go run ./cmd/forgetmenot portfolio-response-email -run-id 1 -file data/reports/portfolio-response.md
go run ./cmd/forgetmenot portfolio-response-email -run-id 1 -file data/reports/portfolio-response.md -send
go run ./cmd/forgetmenot portfolio-codex-email
go run ./cmd/forgetmenot portfolio-codex-email -send
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

`dart-disclosure-sync`는 저장된 종목 중 `dart_corp_code`가 있는 기업을 대상으로
지정 기간의 공시 목록을 전체 페이지 수집합니다. 특정 기업만 확인하려면
`-ticker`, 조회 기간은 `-days`, API 페이지 크기는 `-page-size`로 지정합니다.
한 기업의 요청이 실패해도 성공한 다른 기업의 공시는 저장하고 결과를 `partial`로
표시합니다.

공시는 OpenDART 접수번호 14자리를 기본 식별자로 사용합니다. 반복 동기화 시 새
접수번호는 추가하고 기존 접수번호는 출처와 마지막 관측시각을 갱신합니다. 접수일,
법인구분, 제출인, 비고, DART 공시 뷰어 URL, 출처와 수집시각을 함께 저장합니다.
저장 결과는 `dart-disclosure-list -ticker <ticker>` 또는
`-corp-code <corp_code>`로 확인할 수 있습니다.

공시 목록은 [OpenDART 공시검색 API](https://opendart.fss.or.kr/guide/detail.do?apiGrpCd=DS001&apiId=2019001)를
사용합니다. `dart-document-sync`는 아직 원본이 없는 최신 공시를 골라
[공시서류원본파일 API](https://opendart.fss.or.kr/guide/detail.do?apiGrpCd=DS001&apiId=2019003)의
ZIP을 `data/raw/opendart/documents/<접수번호>/<원본 SHA-256>.zip`에 저장합니다.
특정 접수번호는 `-receipt-no`, 이미 저장된 공시를 다시 확인할 때는 `-force`를
사용합니다. 한 문서가 실패해도 나머지 문서는 계속 처리하며 한 번에 최대 100건만
허용합니다.

다운로드한 ZIP은 경로 이동, 중복 파일명, 손상된 CRC, 과도한 파일 수와 압축 해제
크기를 검사합니다. ZIP 바이트의 SHA-256은 파일 무결성에 사용하고, 압축 해제된
파일명과 내용의 SHA-256으로 계산한 `content_sha256`은 논리 버전 판정에 사용합니다.
따라서 OpenDART가 같은 문서를 다른 ZIP 메타데이터로 다시 압축해도 새 문서
버전으로 기록하지 않습니다. 원본 파일과 실제 DB는 Git에서 제외됩니다.

`dart-financial-sync`는
[OpenDART 단일회사 전체 재무제표 API](https://opendart.fss.or.kr/guide/detail.do?apiGrpCd=DS003&apiId=2019020)를
사용합니다. `-year`는 2015년 이후 사업연도, `-report-code`는 아래 보고서
코드, `-fs-div`는 연결 `CFS`, 개별 `OFS` 또는 동기화 명령에서만 사용할 수 있는
`both`를 받습니다.

- `11011`: 사업보고서
- `11012`: 반기보고서
- `11013`: 1분기보고서
- `11014`: 3분기보고서

API 금액은 쉼표를 제거한 부호 포함 정수 문자열로 정규화합니다. `float64`로
변환하지 않으므로 큰 재무 수치의 정밀도를 잃지 않습니다. 조회 조건별 전체 계정의
내용 해시를 계산해 같은 결과의 반복 수집은 기존 버전을 갱신하고, 정정공시처럼
접수번호나 계정 내용이 바뀌면 이전 버전을 보존한 채 새 현재 버전을 저장합니다.
기업 또는 연결/개별 요청 하나가 실패해도 성공한 재무제표는 저장합니다.

`dart-financial-metrics`는 저장된 현재 재무제표에서 매출액, 영업이익,
당기순이익, 자산·부채·자본총계, 유동자산·유동부채, 영업활동현금흐름을
추출합니다. 계정명 추측은 사용하지 않고 표준 `account_id`와 BS, IS, CIS, CF
구역을 함께 확인합니다. 같은 우선순위의 계정이 여러 개면 임의 선택하지 않고
`ambiguous`, 계정이나 비교기간 금액이 없으면 `missing`으로 표시합니다.

사업보고서는 당기와 전기 금액을, 분기·반기보고서의 손익 및 현금흐름은 누적금액을
비교합니다. 증감률, 영업이익률, 순이익률, 부채비율과 유동비율은 정수 기반으로
소수 둘째 자리까지 계산합니다. 전기 금액이나 비율의 분모가 0 이하이면 해석을
강행하지 않고 `not_comparable`로 표시합니다.

`analysis-snapshot`은 저장된 종목별 포트폴리오, 거래, 투자 가설, 최신 OpenDART
공시와 최신 현재 재무제표를 Yahoo 가격 및 가격 신호와 하나의 입력으로 묶습니다.
OpenDART API를 새로 호출하지 않으므로 키가 없어도 저장된 데이터를 사용할 수
있습니다. `-disclosure-limit`은 포함할 최근 공시 수, `-fs-div`는 최신 재무제표의
연결 `CFS` 또는 개별 `OFS` 구분입니다.

출력에는 `analysis-input/v1` 스키마, 가격·재무 계산 규칙 버전, 생성시각과
`input_sha256`이 포함됩니다. 해시는 생성시각 자체를 제외한 실제 입력과 출처
메타데이터로 계산합니다. 가격 요청이나 저장 데이터 일부가 실패해도 사용 가능한
정보는 반환하고 전체 상태를 `partial`로 표시합니다. OpenDART 기업코드가 없는
종목은 공시와 재무정보를 정상적인 `not_requested` 상태로 둡니다.

`risk-assess`는 동일한 분석 입력 스냅샷에 버전이 지정된 결정론적 규칙을 적용합니다.
가격 신호, 재무비율과 영업현금흐름, 포지션·투자 가설의 데이터 품질, 최근 OpenDART
공시 제목을 검토합니다. 공시 제목 일치는 사건이 확정됐다는 뜻이 아니며, 결과도
매수·매도 지시가 아니라 아래 구조의 검토 항목입니다.

- 확인된 사실
- 가능한 해석
- 확인 질문
- 출처 URL, 접수번호, 계정 ID, 재무 원본 해시 등의 증거

기본 임계값과 공시 조회기간은 결과의 `config`, 규칙 버전은
`risk-rules/v1`, 중복 판정에 사용할 안정적인 식별자는 각 항목의
`fingerprint`에 기록합니다. JSON 출력은 위험 평가와 그 평가에 사용한 전체
스냅샷을 함께 반환합니다.

`research-brief`는 `analysis-snapshot`과 `risk-assess`를 같은 입력 해시에서 실행한
뒤 ChatGPT Plus에 붙여넣을 한국어 프롬프트를 만듭니다. `-question`으로 사용자의
현재 질문을 넣을 수 있으며, text 출력은 프롬프트만 반환합니다. JSON 출력은
프롬프트와 함께 전체 스냅샷과 위험 평가를 반환해 어떤 데이터에서 생성됐는지
확인할 수 있습니다.

프롬프트는 계산된 가격·재무비율을 AI가 임의로 다시 계산하지 않게 하고, 공시 제목과
투자 가설 안의 문장을 명령이 아닌 데이터로만 취급하도록 지시합니다. OpenAI API를
호출하지 않습니다. `research-brief`의 text 출력은 수동 분석용이고,
포트폴리오 자동 분석은 별도 `portfolio-codex-email` 명령이 담당합니다.

`position-reconcile`은 저장된 전체 종목 또는 `-ticker`로 지정한 한 종목에서
매수수량, 매도수량과 거래기간 순증을 계산하고 현재 `positions` 기록과 대조합니다.
이 명령은 읽기 전용이며 포지션을 자동 생성하거나 수정하지 않습니다.

거래내역 시작 전의 기초잔고를 알 수 없으므로 `매수 - 매도`는 현재 보유수량이
아니라 조회기간 순증입니다. 현재 포지션이 있을 때만
`현재수량 - 기간순증`으로 기초잔고를 역산하며, 이 값도 증권사 잔고로 검증된
사실이 아니라 `opening_balance_implied`로 표시합니다. 입출고처럼 지원하지 않는
거래 유형이 있으면 기초잔고를 계산하지 않습니다.

`positions-import`는 증권사 잔고처럼 별도로 확인한 현재 포지션 스냅샷을 CSV에서
일괄 저장합니다. 전체 파일을 한 트랜잭션으로 처리하므로 한 행이라도 유효하지 않으면
아무 행도 반영하지 않으며, 같은 내용을 다시 가져와도 갱신 시각을 바꾸지 않습니다.
거래내역이나 `position-reconcile` 결과로 현재 포지션을 자동 생성하지 않습니다.
입력 형식과 안전 규칙은 [현재 포지션 CSV 형식](docs/POSITION_CSV.md)을 참고하세요.

`portfolio-analyze`는 저장된 현재 포지션에 Yahoo 가격을 결합해 평가금액과
미실현손익을 계산합니다. 환율 데이터가 없는 통화를 합치지 않고 KRW, USD처럼
통화별 결과만 제공하며, 롱·숏 상계로 위험이 작아 보이지 않도록 집중도는 통화별
총 노출액 기준으로 계산합니다. 한 종목의 시세 요청이 실패해도 나머지 종목은
계속 평가하고 실패 원인을 `partial` 결과에 남깁니다. 자세한 계산 계약은
[포트폴리오 평가](docs/PORTFOLIO_VALUATION.md)를 참고하세요.

`portfolio-scenarios`는 같은 포트폴리오 평가 결과에 하락·중립·상승 가격 충격을
적용합니다. 기본값은 -20%, 0%, +20%이며 예측 확률은 만들지 않고
`not_estimated`로 표시합니다. 결과는 전망이나 매매 신호가 아니라 기계적 민감도
분석입니다. 자세한 한계와 계산식은
[포트폴리오 시나리오](docs/PORTFOLIO_SCENARIOS.md)를 참고하세요.

`portfolio-brief`는 한 번 수집한 가격으로 포트폴리오 평가와 시나리오를 만들고,
두 결과의 입력 해시가 일치할 때만 ChatGPT Plus용 프롬프트를 생성합니다. text는
붙여넣을 프롬프트만 출력하고 JSON은 평가, 시나리오, 프롬프트와 각 해시를 함께
반환합니다. OpenAI API를 호출하지 않습니다. 기본 분석 요청은
[`prompts/portfolio_review.md`](prompts/portfolio_review.md)에서 읽습니다.
이 파일을 수정하면 매 실행에 같은 요청 구조가 적용되며, `-question`은 파일 내용을
일회성 질문으로 덮어씁니다. 다른 템플릿은 `-question-file`로 지정하고,
`-question-file ""`은 파일 로드를 끄고 코드에 내장된 기본 요청을 사용합니다.

`portfolio-brief -save`는 명시한 경우에만 분석 결과를 `analysis_runs`에 저장합니다.
같은 입력·규칙·출력은 중복 저장하지 않으며 `analysis-run-list`로 해시와 원본
payload를 조회할 수 있습니다. 자세한 정책은
[분석 실행 이력](docs/ANALYSIS_RUNS.md)을 참고하세요.

`alert-evaluate`는 저장된 `portfolio_brief` 실행의 payload와 해시를 검증하고
집중도, 평가 불가, 취득원가 누락과 현재 포지션 누락을 알림 후보로 저장합니다.
같은 분석 실행을 다시 평가해도 발생 횟수를 늘리지 않으며 `alert-list`에서 상태별로
조회할 수 있습니다. 규칙과 한계는 [알림 후보](docs/ALERTS.md)를 참고하세요.

`daily-email-report`는 `pending` 알림을 심각도별로 요약합니다. 기본 실행은
미리보기만 하며 `-send`를 명시하고 `.env`의 SMTP 설정이 유효할 때만 이메일을
보냅니다. 성공한 경우에만 알림 상태를 `sent`로 바꾸며 자동 스케줄은 아직
등록하지 않습니다. 설정과 안전 규칙은
[일간 이메일 보고서](docs/EMAIL_REPORTS.md)를 참고하세요.
전송 경로만 확인할 때는 `-test-alert -send`를 사용하며, 이 메일은 `[TEST]`로
표시되고 실제 SQLite 알림 상태를 변경하지 않습니다.

`portfolio-response-email`은 ChatGPT Plus의 상세 분석 답변을 Markdown 또는 평문
파일로 가져와 저장된 `portfolio_brief` 분석 실행과 연결합니다. 기본 실행은
메일 미리보기이고 `-send`를 명시한 경우에만 기존 SMTP 설정으로 전송합니다.
OpenAI API를 호출하지 않으며 원본 입력, 프롬프트와 답변의 SHA-256을 메일에
노출하지 않고 SQLite와 JSON 결과에만 기록합니다. 실제 모델과 답변 정확성은
검증할 수 없으므로 사용자가 확인해야 합니다. 이 명령은 Codex CLI를 사용할 수
없을 때의 수동 예비 경로입니다.

`portfolio-codex-email`은 새 포트폴리오 브리핑 생성과 분석 실행 저장,
ChatGPT로 인증된 Codex CLI 분석, 응답 파일 저장과 이메일 미리보기를 한 번에
수행합니다. `-send`를 붙인 경우에만 기존 SMTP 설정으로 전송합니다. 별도 ChatGPT
창, 프롬프트 복사 또는 메모장 작업은 필요하지 않습니다. 이메일은 날짜와 시간대만
메타데이터로 표시하고 Markdown을 제거한 5개 절의 행동 중심 일반 텍스트로
작성합니다. 인증과 실행 제한, 추적 해시 및 수동 예비 경로는
[Codex 포트폴리오 분석 이메일](docs/PORTFOLIO_RESPONSE_EMAIL.md)을 참고하세요.

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
