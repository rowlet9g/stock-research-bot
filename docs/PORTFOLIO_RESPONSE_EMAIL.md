# 포트폴리오 상세 분석 응답 이메일

`portfolio-response-email`은 `portfolio-brief`가 만든 프롬프트를 ChatGPT Plus에서
수동으로 분석한 뒤, 그 답변을 Markdown 또는 텍스트 파일로 가져와 이메일로
미리보기하거나 전송하는 반자동 명령입니다. OpenAI API를 호출하지 않으므로
ChatGPT Plus 외의 API 사용료는 발생하지 않습니다.

## 처리 흐름

```text
portfolio-brief -save
  -> 저장된 분석 실행 ID와 프롬프트
  -> 사용자가 ChatGPT Plus에 프롬프트 입력
  -> 답변을 로컬 파일로 저장
  -> portfolio-response-email 미리보기
  -> -send를 지정한 경우 SMTP 전송
```

ForgetMeNot은 저장된 `portfolio_brief` 실행의 payload와 해시를 검증하고, 답변
파일을 해당 실행 ID에 연결합니다. 메일에는 아래 추적 정보를 포함합니다.

- 원본 분석 실행 ID와 생성시각
- 포트폴리오 입력 SHA-256
- 저장된 분석 payload SHA-256
- 프롬프트 SHA-256
- 정규화한 답변 본문의 SHA-256
- `manual_chatgpt_plus_import` 반입 방식

답변 파일만으로 실제 ChatGPT 모델, 대화 ID와 답변의 사실 정확성을 검증할 수는
없습니다. 이 한계는 메일 본문에도 표시됩니다.

## PowerShell 실행

먼저 보고서 디렉터리를 만들고 포트폴리오 분석 실행과 프롬프트를 각각 저장합니다.
프롬프트는 클립보드를 거치지 않습니다.

```powershell
New-Item -ItemType Directory -Force data\reports

$brief = go run ./cmd/forgetmenot portfolio-brief -save -output json | ConvertFrom-Json
$runId = $brief.analysis_run.id
$brief.brief.prompt |
  Set-Content -LiteralPath data\reports\portfolio-prompt.md -Encoding utf8
$runId
```

`data/reports/portfolio-prompt.md`를 ChatGPT Plus에 첨부하거나 파일 내용을
입력합니다. 답변을 받기 전에 별도의 응답 파일을 편집기로 열어 둡니다.

```powershell
notepad data\reports\portfolio-response.md
```

그다음 ChatGPT 답변 전체를 복사하고 이미 열어 둔 편집기에 붙여넣은 뒤 저장합니다.
답변을 복사한 다음 `Get-Clipboard | Set-Content` 명령을 다시 복사해 실행하면
클립보드가 명령문으로 덮일 수 있으므로 이 방식을 사용하지 않습니다.

먼저 메일 본문을 미리보기합니다. 미리보기에는 SMTP 설정이 필요하지 않습니다.

```powershell
go run ./cmd/forgetmenot portfolio-response-email `
  -run-id $runId `
  -file data/reports/portfolio-response.md
```

내용과 분석 실행 ID가 맞으면 `.env`의 기존 SMTP 설정으로 전송합니다.

```powershell
go run ./cmd/forgetmenot portfolio-response-email `
  -run-id $runId `
  -file data/reports/portfolio-response.md `
  -send
```

프로그램에서 결과를 읽어야 하면 `-output json`을 추가합니다.

## 입력 및 보안 규칙

- `-run-id`는 저장된 `portfolio_brief` 분석 실행이어야 합니다.
- 답변 파일은 UTF-8 Markdown 또는 평문을 사용합니다.
- 빈 파일과 200자 미만의 응답은 거부하며 최대 크기는 256 KiB입니다.
- `Get-Clipboard`와 `Set-Content` 캡처 명령이 답변 대신 들어간 파일은 거부합니다.
- 원본 프롬프트와 내용이 같은 파일은 답변으로 전송하지 않습니다.
- 줄바꿈은 해시 계산 전에 LF로 정규화합니다.
- 메일은 HTML이 아닌 UTF-8 평문으로 전송합니다.
- `data/reports/`는 Git에서 제외합니다.
- `-send`를 명시하지 않으면 실제 이메일을 보내지 않습니다.
- 현재 전송 이력을 별도로 저장하지 않으므로 같은 명령을 다시 `-send`하면 중복
  메일이 발송될 수 있습니다.

## 현재 한계

- ChatGPT Plus 화면이나 대화 내용을 자동으로 읽지 않습니다.
- 답변의 인용 출처가 실제로 유효한지 자동 검증하지 않습니다.
- Markdown을 HTML 이메일로 렌더링하지 않습니다.
- 답변 파일과 분석 실행의 의미적 일치 여부는 사용자가 확인해야 합니다.
- 완전 자동화에는 별도 과금 API 또는 로컬 모델 연동이 필요합니다.
