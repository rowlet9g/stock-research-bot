# 분석 실행 이력

`analysis_runs`는 분석과 알림의 재현성을 위해 입력 해시, 규칙 버전과 원본 JSON
결과를 SQLite에 저장합니다.

## 저장

분석 명령은 기본적으로 읽기 전용입니다. `portfolio-brief`에 `-save`를 명시한
경우에만 실행 이력을 저장합니다.

```powershell
go run ./cmd/forgetmenot portfolio-brief -save -output text
```

text 출력은 ChatGPT Plus에 붙여넣는 프롬프트만 유지합니다. 저장 결과와 ID까지
확인하려면 JSON 출력을 사용합니다.

```powershell
go run ./cmd/forgetmenot portfolio-brief -save -output json
```

## 조회

```powershell
go run ./cmd/forgetmenot analysis-run-list -kind portfolio_brief
go run ./cmd/forgetmenot analysis-run-list -kind portfolio_brief -include-payload -output json
```

기본 조회는 원본 payload를 생략합니다. `-include-payload`를 지정해야 저장된 전체
JSON을 반환합니다.

## 멱등성과 해시

- `input_sha256`은 분석이 사용한 입력 데이터 버전을 가리킵니다.
- `rule_version`은 결과를 만든 규칙 또는 프롬프트 버전입니다.
- `output_sha256`은 공백을 제거한 원본 JSON payload의 해시입니다.
- `idempotency_key`는 kind, 입력 해시, 규칙 버전과 출력 해시로 계산합니다.

네 값이 같은 실행을 다시 저장하면 새 행을 만들지 않습니다. 입력이나 규칙 버전,
출력 내용이 바뀌면 별도 이력으로 저장합니다.

SQLite 데이터베이스와 실제 분석 payload는 Git에서 제외됩니다. payload에는 실제
포지션과 투자 질문이 포함될 수 있으므로 로그나 공개 저장소에 복사하지 않습니다.
