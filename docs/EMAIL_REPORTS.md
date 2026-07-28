# 일간 이메일 보고서

ForgetMeNot은 `pending` 상태의 알림 후보를 모아 일간 투자 점검 이메일을
생성합니다. 초기 채널은 이메일로 결정했으며 Telegram 연동은 구현 대상에서
제외했습니다.

현재 범위는 다음과 같습니다.

- SMTP 계정 없이 보고서 미리보기
- 실제 투자 데이터와 분리된 `[TEST]` 합성 알림 메일
- TLS가 적용된 SMTP를 통한 명시적 전송
- 심각도별 경고, 관찰, 정보 건수와 사실 요약
- 전송 성공 후 포함된 알림의 `sent` 상태 및 발송시각 저장
- 다음 분석 실행에서 같은 사건이 재관측되면 `pending`으로 다시 열기
- SMTP 실패 시 알림을 `pending`으로 유지

자동 실행 스케줄은 아직 등록하지 않습니다. 실제 계정으로 한 번 전송해 본 뒤
사용자가 원하는 발송시각을 정하고 Windows 작업 스케줄러를 연결합니다.

## 미리보기

미리보기는 `.env`에 이메일 계정 정보가 없어도 실행할 수 있으며 실제 메일을
보내거나 알림 상태를 변경하지 않습니다.

```powershell
go run ./cmd/forgetmenot daily-email-report
go run ./cmd/forgetmenot daily-email-report -output json
```

기본적으로 `pending` 알림을 최대 100건 포함합니다. 최대 500건까지 지정할 수
있습니다.

```powershell
go run ./cmd/forgetmenot daily-email-report -limit 500
```

## 테스트 알림 메일

실제 포트폴리오 위험을 만들거나 SQLite 알림 상태를 변경하지 않고 전송 경로를
검증하려면 `-test-alert`를 사용합니다.

```powershell
# 제목과 본문 미리보기
go run ./cmd/forgetmenot daily-email-report -test-alert

# 합성 알림 1건을 포함한 실제 이메일 전송
go run ./cmd/forgetmenot daily-email-report -test-alert -send
```

테스트 보고서는 제목과 본문에 `[TEST]`를 표시하고, 포함된 항목이 실제
포트폴리오 위험이 아니라는 사실을 명시합니다. 실제 `pending` 알림은 읽거나
`sent`로 변경하지 않습니다. `-test-alert`와 `-send-empty`는 함께 사용할 수
없습니다.

## 계정 설정

`.env.example`을 기준으로 추적되지 않는 `.env`에 아래 값을 설정합니다.

```dotenv
SMTP_HOST=
SMTP_PORT=
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_TLS_MODE=starttls
EMAIL_FROM=
EMAIL_TO=
EMAIL_SUBJECT_PREFIX=[ForgetMeNot]
EMAIL_REPORT_TIMEZONE=Asia/Seoul
```

| 환경변수 | 의미 |
| --- | --- |
| `SMTP_HOST` | 이메일 공급자의 SMTP 서버 호스트 |
| `SMTP_PORT` | SMTP 포트 |
| `SMTP_USERNAME` | SMTP 인증 사용자 |
| `SMTP_PASSWORD` | SMTP 전용 비밀번호 또는 공급자가 발급한 앱 비밀번호 |
| `SMTP_TLS_MODE` | `starttls` 또는 `implicit` |
| `EMAIL_FROM` | 발신 주소. 선택적으로 `이름 <주소>` 형식 사용 |
| `EMAIL_TO` | 쉼표로 구분한 하나 이상의 수신 주소 |
| `EMAIL_SUBJECT_PREFIX` | 제목 접두사 |
| `EMAIL_REPORT_TIMEZONE` | 보고서 날짜와 표시시각의 IANA 시간대 |

일반 웹 로그인 비밀번호를 그대로 사용한다고 가정하지 않습니다. 공급자에 따라
앱 비밀번호 또는 OAuth2만 허용할 수 있으므로 실제 공급자를 정한 뒤 최신 공식
인증 정책을 확인해야 합니다. 현재 어댑터는 TLS 위의 SMTP 사용자명/비밀번호
인증을 지원하며 OAuth2는 지원하지 않습니다.

비밀번호, 전체 설정과 수신 주소는 명령 출력에 포함하지 않습니다. `.env`는
Git에서 제외됩니다.

## 전송

`-send`를 명시한 경우에만 SMTP 설정을 검증하고 메일을 보냅니다.

```powershell
go run ./cmd/forgetmenot daily-email-report -send
```

확인할 `pending` 알림이 없으면 기본적으로 전송을 생략하며 SMTP 설정도 요구하지
않습니다. 계정 연결만 시험하기 위해 빈 보고서를 보내려면 다음처럼 실행합니다.

```powershell
go run ./cmd/forgetmenot daily-email-report -send -send-empty
```

SMTP 전송이 실패하면 알림 상태는 바뀌지 않습니다. SMTP 서버가 메일을
수락했으나 그 뒤 SQLite 상태 갱신이 실패하면 명령은 중복 가능성을 명시한 오류를
반환합니다. 이 경우 `alert-list -status pending`과 실제 수신함을 확인하기 전에
재실행하지 않습니다.

## 일간 실행 전제

보고서에 새 알림이 들어오려면 먼저 새로운 분석 실행과 알림 평가가 필요합니다.

```powershell
go run ./cmd/forgetmenot portfolio-brief -save -output json
go run ./cmd/forgetmenot alert-evaluate -run-id <analysis_run.id>
go run ./cmd/forgetmenot daily-email-report -send
```

자동화 단계에서는 위 세 작업을 하나의 순차 작업으로 묶어야 합니다. 앞 단계가
실패했는데 이전 `analysis_run.id`를 다시 사용하거나, 여러 스케줄러 인스턴스가
동시에 이메일을 보내지 않도록 실행 잠금과 실패 정책이 필요합니다.

## 현재 한계

- Windows 작업 스케줄러 등록은 미구현입니다.
- 실제 이메일 공급자 계정으로 smoke test하지 않았습니다.
- SMTP OAuth2는 지원하지 않습니다.
- HTML 본문, 첨부파일과 상세 분석 링크는 지원하지 않습니다.
- 이메일 전달 이력 전용 테이블과 실패 재시도 횟수는 아직 없습니다.
- 여러 프로세스가 동시에 `-send`를 실행하는 운영은 지원하지 않습니다.
- 주간 보고서와 시장·공시 종합 요약은 후속 범위입니다.
