# Whalechime

Docker의 변화를 수집하고, 각 사용자가 선택한 소식을 원하는 곳으로 보내는 알림 서비스입니다.

- **중앙 서버:** Go, PostgreSQL, 내장 웹 설정 화면
- **수집 에이전트:** Docker 이벤트 전체 수집, 디스크 재전송 대기, 읽기 전용 컨테이너 로그 중계
- **사용자 PC 트레이:** 서버 → 컨테이너 하위 메뉴 → 선택한 컨테이너의 실시간 로그
- **개인 설정:** 수신 항목과 수신처를 독립적으로 관리
- **담당자 연결:** 서버별 사용자 직접 연결과 팀 연결

## 중앙 서버 실행

Docker와 Docker Compose, OpenSSL을 준비합니다.

```sh
bash scripts/configure-server.sh
docker compose --env-file .local/server/server.env -f deploy/server.compose.yaml up --build -d
```

브라우저에서 `http://localhost:8080`을 열고 `.local/server/server.env`의 `WHALECHIME_ADMIN_TOKEN` 값으로 로그인합니다. 이 파일에는 DB 암호와 수신처 암호화 키도 들어 있으므로 PostgreSQL 데이터와 함께 안전하게 보관합니다.

기본 포트는 호스트의 `127.0.0.1`에만 공개합니다. 원격 접속은 TLS 리버스 프록시를 사용하고 필요하면 설정 파일의 `WHALECHIME_BIND_ADDRESS`, `WHALECHIME_PORT`를 변경합니다. 에이전트와 트레이에는 접속 가능한 중앙 서버 주소를 지정합니다.

1. **관리**에서 사용자를 등록하고 발급된 사용자 토큰을 해당 사용자에게 전달합니다.
2. 감시 서버를 등록하고 발급된 에이전트 토큰으로 해당 서버에 에이전트를 실행합니다.
3. 서버에 관련 사용자를 직접 연결하거나, 팀에 사용자를 추가하고 담당 팀을 서버에 연결합니다.
4. 사용자는 자신의 토큰으로 로그인해 **수신 항목**과 **내 수신처**를 설정합니다.

새 계정의 기본값은 알림 수신 꺼짐입니다. 수신처를 추가할 때 사용 여부를 켜고 필요한 이벤트 항목도 켜야 발송됩니다. 사용자와 에이전트 토큰은 서로 호환되지 않습니다. 일반 사용자 토큰은 관리 화면에서 재발급할 수 있고, 초기 관리자 토큰은 서버 설정 파일에서 변경한 뒤 서버를 재시작합니다.

## 에이전트와 트레이

- [에이전트 설치·실행](docs/agent-installation.md): 기존 Docker 권한을 사용하며 호스트 사용자·그룹·소켓·시스템 서비스를 자동 변경하지 않습니다.
- [트레이와 로그 전용 보기](docs/tray.md): 우클릭 메뉴에서 서버와 컨테이너를 선택하면 최근 200줄과 이후 로그가 자동 표시됩니다.
- 트레이의 **웹으로 이동**은 일반 웹 진입, **설정**은 중앙 웹의 개인 수신 설정으로 이동합니다.
- 컨테이너 로그는 읽기 전용으로 중계합니다. 컨테이너·이미지 생성, 배포, 시작, 중지, 삭제 등의 제어 기능은 없습니다.

## 알림 채널

이메일(SMTP), Slack, Microsoft Teams Workflows, Discord, Telegram, Google Chat, Mattermost, ntfy, Gotify, Pushover, 사용자 정의 HTTP Webhook을 제공합니다. 채널별 입력은 웹 화면에 표시하며 [연동 안내](docs/notifications.md)에서 설정 조건을 확인할 수 있습니다.

이벤트별로 채널을 지정하지 않습니다. 선택한 이벤트는 해당 사용자의 활성 수신처 모두로 발송합니다. 같은 종류의 수신처도 여러 개 등록할 수 있습니다. 연결 정보는 AES-256-GCM으로 암호화하고 조회 API에는 반환하지 않습니다.

## 수집·발송 동작

- Docker가 제공하는 객체 종류·동작을 필터 없이 수집합니다. 이미지 pull·push도 포함합니다.
- 개인 알림을 꺼도 중앙의 이벤트 기록은 유지됩니다. Docker에서 처음 관찰한 새 이벤트도 설정 목록에 추가됩니다.
- 같은 서버의 동일 이벤트 재전송은 중복 기록과 중복 발송 대기를 만들지 않습니다.
- 이벤트와 발송 대기를 같은 DB 트랜잭션으로 저장하고, 실패한 수신처만 최대 8회 시도합니다.
- 발송 직전에 사용자·팀의 서버 접근 권한과 최신 수신 설정을 다시 확인합니다.
- 외부 서비스가 접수한 직후 응답 또는 DB 연결이 끊기면 동일 알림이 다시 전송될 수 있습니다. 외부 발송은 정확히 한 번 전송을 보장하지 않습니다.
- 에이전트가 수집한 미확인 이벤트는 로컬 디스크에 남습니다. 다만 Docker 자체의 과거 이벤트 재조회는 최근 256개로 제한되므로 장시간 Docker 연결이 끊긴 구간의 완전한 복구를 보장하지 않습니다. [Docker 문서](https://docs.docker.com/reference/cli/docker/system/events/)
- 중앙 서버는 현재 한 인스턴스로 실행합니다. 읽기 전용 로그 중계 작업은 해당 프로세스의 메모리에 있습니다.
- 이미지 저장소에서만 일어난 작업은 해당 Docker 서버에서 관찰되지 않습니다. 이 버전은 등록한 Docker 서버의 이벤트를 수집하며 레지스트리 전용 Webhook 수집은 포함하지 않습니다.
- 이벤트 이력과 컨테이너 stdout·stderr는 별개입니다. 컨테이너 출력 전체를 PostgreSQL에 장기 저장하지 않습니다.

## 개발과 검증

Go 1.25 이상을 사용합니다. 중앙 DB는 PostgreSQL 17을 기준으로 검증합니다.

```sh
go mod download
make build
make test
make vet
```

실제 PostgreSQL 통합 테스트는 전용 DB URL을 지정합니다. 테스트마다 별도 스키마를 만들고 그 스키마만 제거합니다.

```sh
export WHALECHIME_TEST_DATABASE_URL='postgres://사용자:암호@localhost:5432/테스트DB?sslmode=disable'
make test-integration
```

웹 회귀 테스트는 **전용 테스트 중앙 서버**를 먼저 실행한 뒤 사용합니다. 테스트는 계정·서버·팀을 생성하고 관리자 개인 설정을 변경하므로 운영 서버를 대상으로 실행하지 않습니다.

```sh
npm ci
npx playwright install chromium
export WHALECHIME_TEST_BASE_URL=http://127.0.0.1:18086
export WHALECHIME_TEST_ADMIN_TOKEN='테스트 서버 관리자 토큰'
npm run test:ui
```

테스트 서버에는 `WHALECHIME_ALLOWED_NOTIFICATION_HOSTS=127.0.0.1`을 설정합니다. 외부 메신저 계정으로 실제 알림을 보내지 않고 로컬 수신 서버에서 테스트 발송을 확인합니다. Go·PostgreSQL·웹·트레이 검증은 `.github/workflows/ci.yml`에 포함돼 있습니다.

자세한 환경변수와 API는 [운영 안내](docs/operations.md), [API 안내](docs/api.md), 전체 제품 범위는 [요구사항](docs/requirements.md)을 참고하세요.

## 라이선스

[Apache License 2.0](LICENSE)
