# 투자 프로필

`data/investment_profile.json`은 포트폴리오 목표 배분, 운용 규칙과 종목별 투자
가설을 한 파일에서 관리합니다. 실제 파일은 개인 포트폴리오 정보이므로 Git에서
제외하고, 형식 예제만 `data/investment_profile.example.json`으로 커밋합니다.

새 환경에서는 예제 파일을 복사한 뒤 실제 목표와 가설을 채웁니다.

```powershell
Copy-Item data/investment_profile.example.json data/investment_profile.json
```

## 기본 흐름

프로필만 검증하고 SQLite에 투자 가설을 동기화하려면 다음 명령을 사용합니다.

```powershell
go run ./cmd/forgetmenot investment-profile-sync
```

`portfolio-brief`와 `portfolio-codex-email`은 기본적으로 같은 프로필 파일을 먼저
검증하고 동기화합니다. 따라서 종목마다 `thesis-set`을 반복할 필요가 없습니다.

```powershell
go run ./cmd/forgetmenot portfolio-brief -output text
go run ./cmd/forgetmenot portfolio-codex-email
go run ./cmd/forgetmenot portfolio-codex-email -send
```

다른 파일은 `-profile <path>`로 지정하고, `-profile ""`은 자동 로드를 끕니다.
프로필에 존재하지 않는 ticker가 있거나 JSON 형식과 목표 배분이 잘못되면 분석을
시작하지 않습니다.

## 저장 규칙

- `version`은 현재 `investment-profile/v2`입니다.
- `allocations[].target_percent`의 합은 정확히 100이어야 합니다.
- `theses[].allocation_category`는 allocations에 정의된 category여야 합니다.
- `protected_quantity`는 0 이상의 소수이며 해당 수량까지 축소 검토에서 제외합니다.
- `increase_condition`은 추가 매수를 검토하기 전에 확인할 조건입니다.
- 같은 ticker를 두 번 쓰거나 알 수 없는 JSON 필드를 추가하면 검증에 실패합니다.
- 동기화는 하나의 SQLite 트랜잭션으로 수행하며 같은 파일을 반복 실행해도 중복되지
  않습니다.

프로필의 목표 수익률과 목표 배분은 분석 기준이지 수익 보장이나 자동 주문 조건이
아닙니다. 통화 환산 데이터가 없으므로 현재 자산군 비중은 KRW와 USD를 합산하지
않고 각 통화의 총노출 안에서 계산합니다.

## 리밸런싱 정책

`portfolio_policy.rebalance_policy`는 목표 배분에 접근하는 과정에서 매도 권고가
지켜야 할 한계를 정의합니다.

- `mode`는 현재 `cash_flow_first`만 지원합니다. 기존 보유분 매도보다 신규 자금을
  부족한 자산군에 배정하는 방법을 먼저 사용합니다.
- `preferred_max_realized_loss_percent`는 리밸런싱 매도에서 선호하는 최대 손실률의
  절댓값입니다. `7`은 손실률 `-7%` 이내의 매도를 선호한다는 뜻입니다.
- `hard_max_realized_loss_percent`는 가설이 유지되는 종목의 매도 권고가 넘을 수
  없는 손실률의 절댓값입니다. `10`이면 `-10%`보다 손실이 큰 부분·전량 매도
  권고를 검증 단계에서 거부합니다.
- `realized_loss_limit_basis`는 현재 `each_position_cost_basis`만 지원합니다. 저장된
  종목별 평균단가 기준 미실현 수익률을 매도 시 예상 손실률로 사용합니다. 세금,
  수수료와 개별 세부 체결 lot은 아직 반영하지 않습니다.
- `thesis_invalidation_overrides_limit`가 `true`이면 조사 결과 투자 가설이
  `broken`으로 판정된 종목은 손실 한도보다 위험 제거를 우선할 수 있습니다.
- `max_turnover_percent`는 허용 상한입니다. `100`은 필요하면 전체 교체도 검토할
  수 있다는 뜻이지 전량 매도를 목표로 한다는 뜻이 아닙니다.
- `target_horizon_months`는 목표 배분에 접근할 기간입니다.
- `force_target_allocation_by_deadline`가 `false`이면 기간이 끝나도 손실 한도를
  깨면서 목표 비중을 강제로 맞추지 않습니다.

이 손실 한도는 일반적인 손절선이 아닙니다. 오직 가설이 유지되는 종목을
리밸런싱만을 이유로 손실 매도하는 경우에 적용합니다. UFO처럼 "본절 도달 시
전량 매도 후 재편입 금지"와 같은 종목별 규칙은 집중도 한도가 아니라 해당
`theses[]`의 보유·청산 가설로 작성합니다.
