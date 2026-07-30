# Codex 구독 기반 포트폴리오 분석 이메일

`portfolio-codex-email`은 포트폴리오 가격 수집, 평가와 시나리오 생성, 분석 실행
저장, Codex 분석, 응답 파일 저장과 이메일 전송을 한 명령으로 연결합니다. 별도의
ChatGPT 대화, 프롬프트 복사, 메모장 붙여넣기는 필요하지 않습니다.

이 명령은 OpenAI Platform API를 직접 호출하지 않습니다. 실행 직전에 Codex CLI의
인증 상태가 `Logged in using ChatGPT`인지 확인하고, 자식 프로세스에서
`OPENAI_API_KEY`와 `CODEX_API_KEY`를 제거합니다. 따라서 Platform API 키 기반
사용량 과금 경로를 사용하지 않고 로그인한 ChatGPT 플랜의 Codex 사용량 한도를
사용합니다.

- [Codex 인증](https://developers.openai.com/codex/auth)
- [Codex 비대화형 실행](https://developers.openai.com/codex/noninteractive)

## 준비

공식 Codex CLI를 설치하고 ChatGPT 계정으로 로그인합니다.

```powershell
npm install -g @openai/codex
codex login
codex login status
```

마지막 명령의 결과가 `Logged in using ChatGPT`여야 합니다. API 키 로그인 상태나
로그아웃 상태에서는 `portfolio-codex-email`이 분석을 시작하지 않습니다.

이메일 전송에는 기존 [일간 이메일 보고서](EMAIL_REPORTS.md)의 SMTP 설정을
그대로 사용합니다. 미리보기에는 SMTP 설정이 필요하지 않습니다.

기본 투자 정책과 종목별 가설은
[`data/investment_profile.json`](INVESTMENT_PROFILE.md)에서 읽습니다. 명령은
분석 전에 파일을 검증하고 SQLite에 동기화하며, 보호 수량, 증액 조건과
리밸런싱 손실 한도를 분석 입력에 포함합니다.

## 실행

분석 결과를 만들고 로컬 파일에 저장한 뒤 메일 본문을 미리보기합니다.

```powershell
go run ./cmd/forgetmenot portfolio-codex-email
```

내용을 실제 이메일로 보내려면 `-send`만 추가합니다.

```powershell
go run ./cmd/forgetmenot portfolio-codex-email -send
```

기본 질문은 `prompts/portfolio_review.md`에서 읽습니다. 일회성 질문이나 다른
질문 파일도 지정할 수 있습니다.

```powershell
go run ./cmd/forgetmenot portfolio-codex-email `
  -question "현재 포트폴리오에서 가장 먼저 재검토할 가설은?" `
  -send

go run ./cmd/forgetmenot portfolio-codex-email `
  -question-file prompts/portfolio_review.md `
  -codex-reasoning high `
  -output json
```

Codex 분석 제한시간은 기본 15분이고 추론 강도는 `high`입니다.
`-codex-timeout`으로 30초에서 30분 사이를, `-codex-reasoning`으로 추론 강도를
조정할 수 있습니다. 실시간 검색 대상이 많으므로 낮은 추론 강도는 출처 누락이나
응답 검증 실패 가능성을 높일 수 있습니다. 기본 응답 파일은
`data/reports/portfolio-response-latest.txt`이고 Git에서 제외됩니다.

## 처리 흐름

```text
저장된 포지션
  -> 투자 프로필 검증과 가설 동기화
  -> Yahoo 가격 수집
  -> 포트폴리오 평가와 시나리오
  -> portfolio_brief 분석 실행 저장
  -> 격리된 임시 디렉터리에서 실시간 검색을 켠 codex exec 실행
  -> 구조화 응답, 활성 종목, 보호 수량, 매도 손실 한도, 출처와 URL 형식 검증
  -> 검증된 결과를 일반 텍스트 보고서로 변환
  -> data/reports 응답 파일 저장
  -> 보고서 미리보기
  -> -send를 지정한 경우 SMTP 전송
```

Codex는 `--search`, `--ephemeral`, `--ignore-user-config`,
`--sandbox read-only`, `--output-schema`로 실행합니다. 저장소 대신 빈 임시
디렉터리를 작업 위치로 사용하며 셸 명령과 로컬 파일 접근은 금지하고 실시간 웹
검색만 허용합니다. 포트폴리오 사실은 생성된 프롬프트 본문으로만 전달됩니다.

조사 출처는 아래 순서로 우선합니다.

- 국내 기업: [OpenDART](https://opendart.fss.or.kr/intro/infoApiList.do), 회사
  IR·공식 보도자료, KRX
- 미국 기업: [SEC EDGAR](https://www.sec.gov/search-filings/edgar-application-programming-interfaces),
  회사 IR
- ETF: 운용사 공식 상품 페이지, 투자설명서와 보유종목 자료
- 거시 지표: 중앙은행, [FRED](https://fred.stlouisfed.org/docs/api/fred/overview.html),
  미국 재무부 등 공식 통계
- 뉴스: 최근 사건의 맥락과 반론을 위한 보조 출처

SQLite 분석 실행과 JSON 결과에는 아래 추적 정보를 포함합니다.

- 원본 분석 실행 ID와 생성시각
- 포트폴리오 입력 SHA-256
- 저장된 분석 payload SHA-256
- 프롬프트 SHA-256
- 정규화한 응답 본문의 SHA-256
- `codex_cli_chatgpt_web_research` 응답 생성 방식

사람이 읽는 이메일에는 내부 추적값을 노출하지 않습니다. 제목 아래에는 보고서
날짜와 기준 시간대만 표시하고, 이후에는 분석 본문이 바로 이어집니다.

기본 분석은 행동 중심 형식을 사용합니다.

- 핵심 결론과 구체적인 포트폴리오 조정 순서
- 모든 활성 종목에 `추가 매수`, `보유`, `부분 매도`, `전량 매도` 중 하나의 의견
- 종목별 구체적인 조정안, 투자 가설 판정, 증액 조건 판정과 물타기 판단
- 현재 포트폴리오에 맞는 신규 편입 후보 2~4개와 편입 방식
- 앞으로 사용자가 확인할 숙제가 아니라 이번 조사에서 확인한 핵심 사실
- 종목과 후보마다 출처 2~5개, 그중 하나 이상은 1차 출처로 분류된 항목

## 검증 및 보안 규칙

- 분석 전에 새 가격 스냅샷과 포트폴리오 브리핑을 생성하고 SQLite에 저장합니다.
- Codex 원본 응답은 최대 256 KiB이며 지정된 JSON Schema를 따라야 합니다.
- 수량이 0이 아닌 모든 활성 종목이 정확히 한 번 포함되어야 합니다.
- 권고 수량 변화의 부호가 추가 매수·보유·부분 매도·전량 매도 의견과 일치해야 합니다.
- 부분 매도 후 잔량은 0보다 크고 보호 수량 이상이어야 하며, 전량 매도는 현재
  수량 전체를 정확히 닫아야 합니다.
- 가설이 유지되는 종목의 예상 매도 손실률이 프로필의
  `hard_max_realized_loss_percent`를 넘으면 응답을 거부합니다. 프로필이 허용하고
  조사 결과 가설이 `broken`으로 판정된 경우에만 이 한도를 넘을 수 있습니다.
- 매도 손실률을 계산할 평균단가가 없으면 해당 매도 권고를 검증할 수 없으므로
  응답을 거부합니다.
- 신규 후보는 2~4개여야 하며 종목·후보마다 1차 출처로 분류된 항목이 하나 이상
  필요합니다.
- 출처 날짜는 `YYYY-MM-DD`, URL은 유효한 HTTP(S) 형식이어야 합니다.
- 입력 프롬프트를 그대로 되돌린 응답과 클립보드 캡처 명령은 거부합니다.
- 응답이 검증되고 파일 저장까지 성공한 뒤에만 이메일을 만듭니다.
- `-send`를 지정하면 SMTP 설정을 분석 시작 전에 검증합니다.
- `-send`가 없으면 이메일을 전송하지 않습니다.
- 메일과 저장 응답은 UTF-8 일반 텍스트입니다.
- Codex 답변은 투자 참고 자료입니다. 사실 정확성이나 미래 수익을 보장하지 않습니다.
- 같은 명령을 다시 `-send`하면 별도의 이메일이 다시 발송될 수 있습니다.

## 수동 예비 경로

`portfolio-response-email`은 Codex CLI를 사용할 수 없는 환경을 위한 예비
명령입니다. 사용자가 직접 만든 응답 파일과 저장된 `portfolio_brief` 실행 ID를
연결해 미리보기하거나 전송합니다.

```powershell
go run ./cmd/forgetmenot portfolio-response-email `
  -run-id 1 `
  -file data/reports/portfolio-response.md
```

이 경로는 자동 BOT 흐름이 아니며 기본 운영 방식으로 사용하지 않습니다.

## 현재 한계

- ChatGPT 플랜별 Codex 사용량 한도와 일시적인 서비스 제한의 영향을 받습니다.
- 기본 `high` 추론과 실시간 검색은 단순 요약보다 플랜 사용량을 더 많이 소모할 수
  있습니다.
- 출처의 형식과 1차 출처 포함 여부는 검증하지만, 인용 내용이 원문과 정확히
  일치하는지는 아직 별도 코드로 재검증하지 않습니다.
- 현재 전송 이력을 별도 테이블에 저장하지 않습니다.
- 이번 명령은 Codex의 일회성 웹 리서치로 최신 근거를 가져옵니다. OpenDART,
  SEC, FRED와 뉴스 자료를 구조화해 SQLite에 장기 저장하고 변화 이력을 비교하는
  공급자별 수집기는 별도 후속 작업입니다.
