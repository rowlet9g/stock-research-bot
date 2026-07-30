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

Codex 분석 제한시간은 기본 10분이며 `-codex-timeout`으로 30초에서 30분 사이로
조정할 수 있습니다. 기본 응답 파일은
`data/reports/portfolio-response-latest.md`이고 Git에서 제외됩니다.

## 처리 흐름

```text
저장된 포지션
  -> Yahoo 가격 수집
  -> 포트폴리오 평가와 시나리오
  -> portfolio_brief 분석 실행 저장
  -> 격리된 임시 디렉터리에서 codex exec 실행
  -> 응답 형식과 프롬프트 오반입 검증
  -> data/reports 응답 파일 저장
  -> 보고서 미리보기
  -> -send를 지정한 경우 SMTP 전송
```

Codex는 `--ephemeral`, `--ignore-user-config`, `--sandbox read-only`로 실행합니다.
저장소 대신 빈 임시 디렉터리를 작업 위치로 사용하며 웹 검색, 셸 명령과 파일 접근을
하지 말라는 분석 전용 지시를 전달합니다. 포트폴리오 사실은 생성된 프롬프트
본문으로만 전달됩니다.

SQLite 분석 실행과 JSON 결과에는 아래 추적 정보를 포함합니다.

- 원본 분석 실행 ID와 생성시각
- 포트폴리오 입력 SHA-256
- 저장된 분석 payload SHA-256
- 프롬프트 SHA-256
- 정규화한 응답 본문의 SHA-256
- `codex_cli_chatgpt` 응답 생성 방식

사람이 읽는 이메일에는 내부 추적값을 노출하지 않습니다. 제목 아래에는 보고서
날짜와 기준 시간대만 표시하고, 이후에는 분석 본문이 바로 이어집니다.

기본 분석은 행동 중심 형식을 사용합니다.

- 전체 2,500~4,000자
- 핵심 결론, 즉시 행동안, 활성 종목 판단, 추가 매수, 다음 확인사항의 5개 절
- `high` 종목의 40% 기준 재배분액을 1차 위험관리안으로 제시
- 종목별 가격 타이밍, 기본 조치와 판단 변경 조건을 한 줄로 요약
- 별도 데이터 범위, 강점, 가격 출처와 일반론 절은 생략
- KRW는 정수, USD는 소수점 둘째 자리로 표시

## 검증 및 보안 규칙

- 분석 전에 새 가격 스냅샷과 포트폴리오 브리핑을 생성하고 SQLite에 저장합니다.
- Codex 응답은 200자 이상, 최대 256 KiB여야 합니다.
- 입력 프롬프트를 그대로 되돌린 응답과 클립보드 캡처 명령은 거부합니다.
- 응답이 검증되고 파일 저장까지 성공한 뒤에만 이메일을 만듭니다.
- `-send`를 지정하면 SMTP 설정을 분석 시작 전에 검증합니다.
- `-send`가 없으면 이메일을 전송하지 않습니다.
- 메일은 UTF-8 평문이며 Markdown 제목, 강조, 코드 표시와 표 구분자를 일반
  텍스트로 변환합니다.
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
- 답변의 사실 정확성과 인용 출처 유효성을 자동 검증하지 않습니다.
- 현재 전송 이력을 별도 테이블에 저장하지 않습니다.
- 현재 입력만으로 집중도와 가격 추세에 근거한 기본 위험관리 조치는 제시할 수
  있습니다. 기업가치 기반의 매수가 적정성이나 장기 보유 판단은 OpenDART
  재무지표, SEC, 산업, 투자 가설과 검증된 환율 데이터가 추가되어야 강화됩니다.
