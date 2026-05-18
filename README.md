# AI Stock Research Bot

DART 공시/재무 정보와 Yahoo Finance 시세 데이터를 결합해 투자 공부용 리서치 봇을 만드는 프로젝트입니다.

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

## Go 실행

Go 1.22 이상이 필요합니다.

```powershell
go run ./cmd/forgetmenot -watchlist data/watchlist.example.csv -name 삼성전자 -thesis "실적 턴어라운드 기대"
```

`OPENAI_API_KEY` 자동 호출은 아직 Go 포트에 넣지 않았습니다. Plus 요금제 안에서 쓰는 흐름은 앱이 프롬프트를 생성하고 사용자가 ChatGPT에 붙여넣는 방식으로 둡니다.

## Python 프로토타입 실행

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

`dart_corp_code`는 OpenDART의 고유번호입니다. 정확한 매핑 테이블은 별도 수집 기능으로 추가하는 것이 안전합니다.

## 추천 원칙

AI 출력은 항상 아래처럼 분리합니다.

- 사실: 가격, 공시 제목, 재무 수치처럼 데이터로 확인되는 내용
- 해석: 수치와 이벤트가 의미할 수 있는 가능성
- 확인 질문: 투자 가설이 아직 유효한지 검증하는 질문
