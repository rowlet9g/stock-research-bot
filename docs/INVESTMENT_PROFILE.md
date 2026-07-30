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

- `version`은 현재 `investment-profile/v1`입니다.
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
