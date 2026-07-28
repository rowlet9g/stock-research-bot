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
- OpenDART 공시 목록 전체 페이지 동기화와 SQLite 저장
- 공시 접수번호 기반 멱등 저장과 저장 결과 조회
- OpenDART 공시 원본 ZIP 검증, 로컬 저장과 내용 기반 버전 추적
- OpenDART 전체 재무제표 계정 정규화와 내용 기반 버전 저장
- OpenDART 표준계정 기반 핵심 재무지표와 재무비율 계산
- 가격, 포트폴리오, 공시와 재무정보의 통합 분석 입력 스냅샷
- 출처 증거와 확인 질문을 포함하는 결정론적 위험 규칙 평가
- ChatGPT Plus에 붙여넣는 통합 리서치 브리핑 프롬프트
- 거래기간 수량 변화와 저장 포지션의 읽기 전용 대조

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
go run ./cmd/forgetmenot dart-disclosure-sync -days 30
go run ./cmd/forgetmenot dart-disclosure-list -ticker 005930 -limit 20
go run ./cmd/forgetmenot dart-document-sync -ticker 005930 -limit 10
go run ./cmd/forgetmenot dart-document-list -receipt-no 20260727000099
go run ./cmd/forgetmenot dart-financial-sync -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
go run ./cmd/forgetmenot dart-financial-list -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS -account-limit 100
go run ./cmd/forgetmenot dart-financial-metrics -ticker 005930 -year 2025 -report-code 11011 -fs-div CFS
go run ./cmd/forgetmenot analysis-snapshot -ticker 005930 -output json
go run ./cmd/forgetmenot risk-assess -ticker 005930 -output json
go run ./cmd/forgetmenot research-brief -ticker 005930 -question "투자 가설이 유효한가?" -output text
go run ./cmd/forgetmenot position-reconcile -output json
go run ./cmd/forgetmenot positions-import -file data/positions.csv
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자
```

`dart-document-sync`는 공시 원본 ZIP을 기본
`data/raw/opendart/documents` 아래에 저장하고 SQLite에는 원본 파일 해시,
압축 해제된 내용 해시, 개별 파일 해시와 출처 시각을 기록합니다. 같은 내용이
다른 ZIP 바이트로 재전송되어도 논리 버전은 하나로 유지합니다. 저장 루트와
데이터베이스는 Git에서 제외됩니다.

`dart-financial-sync`는 OpenDART 전체 재무제표 계정을 CFS 또는 OFS 단위로
수집합니다. 금액은 정밀도를 보존하는 정수 문자열로 저장하고 전체 계정의 내용
해시가 달라질 때만 새 버전을 만듭니다. 보고서 코드는 `11011` 사업보고서,
`11012` 반기보고서, `11013` 1분기보고서, `11014` 3분기보고서를 사용합니다.

`dart-financial-metrics`는 저장된 현재 버전에서 표준 `account_id`와 재무제표
구역을 함께 확인해 핵심 계정을 선택합니다. 계정명 추측이나 중복 계정 임의 선택은
하지 않습니다. 분기·반기 손익과 현금흐름은 누적금액을 사용하며, 비교 기준이 0
이하인 증감률은 `not_comparable`로 반환합니다.

`analysis-snapshot`은 Yahoo 가격과 신호, 저장된 종목별 포트폴리오, 최신 공시,
최신 현재 재무제표 지표를 `analysis-input/v1` 구조로 결합합니다. 생성시각과 별도로
입력 해시와 계산 규칙 버전을 기록하며, 일부 데이터가 없으면 `partial`과 원인을
반환합니다. OpenDART 대상이 아닌 종목의 공시와 재무는 `not_requested`입니다.

`risk-assess`는 스냅샷과 `risk-rules/v1` 규칙을 함께 반환합니다. 재무 임계값,
포지션·가설 데이터 품질, 가격 신호와 최근 공시 제목을 검토하되, 사실과 가능한
해석 및 확인 질문을 분리합니다. 동일 사건의 `fingerprint`는 평가시각이 달라도
유지되어 이후 중복 알림 억제에 사용할 수 있습니다.

`research-brief`는 같은 입력 해시의 스냅샷과 위험 평가를 ChatGPT Plus용 프롬프트로
구성합니다. text는 붙여넣기용 프롬프트만, JSON은 원본 스냅샷·평가·프롬프트를 함께
반환합니다. OpenAI API를 호출하지 않습니다.

`position-reconcile`은 매수·매도 수량으로 거래기간 순증을 계산하되 이를 현재
보유수량으로 간주하지 않습니다. 기초잔고가 없기 때문입니다. 저장된 현재 포지션이
있을 때만 역산 기초잔고를 참고값으로 제공하며 실제 데이터를 수정하지 않습니다.

`positions-import`는 별도로 확인한 현재 포지션 CSV를 한 트랜잭션으로 저장합니다.
종목 중복, 미등록 종목 또는 잘못된 값이 하나라도 있으면 전체 import를 취소하고,
동일한 스냅샷을 다시 가져오면 저장된 갱신 시각을 유지합니다. 자세한 형식은
[`docs/POSITION_CSV.md`](docs/POSITION_CSV.md)에 정리되어 있습니다.

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
