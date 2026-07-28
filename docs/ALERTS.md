# 알림 후보

ForgetMeNot의 알림 기반은 저장된 포트폴리오 분석 실행에서 확인할 사건을
결정론적으로 만들고 SQLite에 중복 없이 기록합니다. 현재 범위는 후보 생성과
조회까지이며 Telegram, 이메일 등 외부 채널로 발송하지 않습니다.

## 실행 순서

먼저 동일한 가격 입력으로 포트폴리오 평가, 시나리오와 ChatGPT Plus용 브리핑을
만들어 분석 실행을 저장합니다.

```powershell
go run ./cmd/forgetmenot portfolio-brief -save -output json
```

JSON의 `analysis_run.id`를 사용해 알림 후보를 평가하고 저장합니다.

```powershell
go run ./cmd/forgetmenot alert-evaluate -run-id 1
go run ./cmd/forgetmenot alert-list -status pending
go run ./cmd/forgetmenot alert-list -include-payload -output json
```

`alert-evaluate`는 저장된 실행이 `portfolio_brief`인지 확인하고, 저장된
`input_sha256` 및 `output_sha256`과 payload를 다시 대조합니다. 검증에 실패한
분석 결과로는 알림 후보를 만들지 않습니다.

## 현재 규칙

규칙 버전은 `portfolio-alert-candidates/v1`입니다.

| 규칙 | 심각도 | 의미 |
| --- | --- | --- |
| `portfolio.position_concentration` | `watch` 또는 `warning` | 통화별 총 노출액 기준 단일 종목 비중이 설정 임계값 이상 |
| `portfolio.position_unvalued` | `watch` | 보유수량은 있지만 가격 등의 누락으로 평가금액 계산 불가 |
| `portfolio.cost_basis_missing` | `info` | 평가금액은 있지만 평균 취득단가가 없어 원가와 손익 계산 불가 |
| `portfolio.current_positions_missing` | `info` | 저장 종목 일부에 확인된 현재 포지션 스냅샷이 없음 |

각 후보는 사실, 가능한 해석, 확인 질문과 계산 증거를 분리해 저장합니다. 후보는
매수·매도 신호가 아니며 사용자가 먼저 검토할 데이터 품질 또는 위험 상태입니다.

## 중복 억제

- `fingerprint`는 규칙 버전, 규칙 ID, ticker와 통화로 계산합니다.
- 관측값이나 평가시각이 달라져도 같은 논리적 사건은 같은 fingerprint를 씁니다.
- 하나의 분석 실행 ID에서 같은 후보를 다시 평가해도 관측을 추가하지 않습니다.
- 새로운 분석 실행에서 같은 사건이 다시 관측되면 기존 알림의 발생 횟수와 최근
  관측시각을 갱신합니다.
- 입력이나 규칙 버전이 다르면 분석 실행 이력에서 별도로 추적할 수 있습니다.

이 구조는 CLI 재시도 때문에 같은 알림이 여러 번 발송되는 일을 막기 위한 저장
기반입니다.

## 현재 한계

- 자동 실행 스케줄러가 없습니다.
- Telegram 및 이메일 전달 adapter가 없습니다.
- 발송 성공, 실패, 확인 상태를 변경하는 명령이 없습니다.
- 발송 완료 후 같은 사건이 다시 발생했을 때 재알림하는 정책이 없습니다.
- quiet hours와 심각도별 채널 정책이 없습니다.
- 환율이 없으므로 서로 다른 통화의 포트폴리오를 합산하지 않습니다.

외부 발송을 붙이기 전에 재발송 상태 전이, 민감정보 축약, 테스트 알림 구분과
실패 재시도 한도를 먼저 정의해야 합니다.
