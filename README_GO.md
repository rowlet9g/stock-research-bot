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

OpenAI API 자동 호출은 아직 Go 포트에 넣지 않았습니다. Plus 요금제 안에서 쓰려면 앱이 프롬프트를 생성하고 사용자가 ChatGPT에 붙여넣는 방식이 추가 과금 없이 가장 안전합니다.

## 실행

Go 1.22 이상이 필요합니다.

```powershell
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자 -thesis "실적 턴어라운드 기대"
```

DART 공시까지 보려면 `.env` 또는 환경변수에 `OPENDART_API_KEY`를 설정하고, 관심종목 CSV의 `dart_corp_code`를 채워야 합니다.

## 테스트

```powershell
$env:GOCACHE = "$PWD\.gocache"
$env:GOMODCACHE = "$PWD\.gomodcache"
go build ./...
go test ./...
go vet ./...
```
