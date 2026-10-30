# 운영과 개발 설정

## 중앙 서버 환경변수

| 키 | 내용 |
| --- | --- |
| `DATABASE_URL` | PostgreSQL 연결 URL. 운영의 원격 DB는 TLS 연결을 설정한다. |
| `WHALECHIME_ADMIN_TOKEN` | 32바이트 이상인 초기 관리자 토큰. 시작 시 해당 계정 토큰을 갱신한다. |
| `WHALECHIME_ENCRYPTION_KEY` | 32바이트를 Base64로 인코딩한 고정 키. 수신처 자격 증명 암호화에 사용한다. |
| `WHALECHIME_LISTEN_ADDR` | 기본 `:8080`. 직접 실행 시 `127.0.0.1:8080`처럼 제한할 수 있다. |
| `WHALECHIME_ALLOWED_NOTIFICATION_HOSTS` | 사설 알림 연동을 허용할 호스트/IP 목록. 기본 빈 값. |

```sh
export DATABASE_URL='postgres://사용자:암호@localhost:5432/whalechime?sslmode=disable'
export WHALECHIME_ADMIN_TOKEN='32바이트 이상의 임의 토큰'
export WHALECHIME_ENCRYPTION_KEY='32바이트의 Base64 인코딩 값'
go run ./cmd/whalechime
```

`scripts/configure-server.sh`는 이 값들을 임의 생성하고 `.local/server/server.env`에 저장한다. 기존 설정을 덮어쓰지 않는다. PostgreSQL 스키마 마이그레이션은 서버 시작 시 트랜잭션·전용 잠금으로 적용한다.

## 상태 확인

- `/healthz`: 프로세스가 HTTP 요청을 처리하는지 확인한다.
- `/readyz`: 2초 안에 PostgreSQL 연결을 확인한다.
- `whalechime -healthcheck http://127.0.0.1:8080/readyz`: 별도 curl 없이 준비 상태를 확인하고 종료한다.
- 에이전트는 30초마다 생존 신호를 보낸다. 웹은 90초 이상 신호가 없으면 연결 확인이 필요하다고 표시한다. 서버 연결 끊김에 대한 별도 합성 알림은 현재 생성하지 않는다.

## 백업과 변경

PostgreSQL 백업과 `WHALECHIME_ENCRYPTION_KEY`를 함께 보관한다. 키가 없으면 저장된 수신처 연결 정보를 복호화할 수 없다. 키 교체 시 기존 수신처를 새 키로 다시 등록하는 절차가 필요하며 자동 키 순환은 제공하지 않는다.

```sh
docker compose --env-file .local/server/server.env -f deploy/server.compose.yaml exec -T postgres pg_dump -U whalechime whalechime > whalechime-backup.sql
```

백업 파일에는 사용자 정보와 암호화된 연결 정보가 있으므로 앱 설정 파일처럼 보호한다. 이벤트와 발송 이력은 자동 삭제하지 않는다. 장기 운영 시 저장량을 확인하고 조직의 보존 정책에 따라 PostgreSQL 백업·정리 절차를 수립한다.

## 실행 구조와 한계

- 중앙 서버 한 인스턴스 안에 API, 내장 웹 화면, 발송 작업자 4개가 있다.
- 에이전트·트레이와 중앙 서버 사이에는 연결 가능한 HTTP(S) 경로가 필요하다. 원격 접속에는 TLS를 사용한다.
- 컨테이너 조회·로그 중계는 중앙 서버 메모리의 요청을 에이전트가 가져가는 방식이다. 에이전트 수신 포트는 없다.
- 에이전트 재연결 시 Docker가 제공하는 최근 이벤트 범위에서 재조회한다. 이미 수집한 미확인 이벤트는 로컬 스풀로 복구한다.
- 에이전트의 데이터 디렉터리는 서버 한 대와 토큰 한 개에 전용으로 사용한다. 다른 Docker 데몬이나 등록 서버로 바꾸면 별도의 디렉터리를 사용한다.
- 스풀의 용량 제한이나 자동 폐기는 하지 않는다. 중앙 연결이 오래 끊기면 디스크 사용량이 늘어난다. 쓰기 실패 시 조용히 버리지 않고 에이전트가 오류로 종료한다.
- 배포용 Compose의 PostgreSQL 볼륨은 `down`으로 남으며 `down -v`는 데이터를 삭제하므로 데이터 폐기가 목적일 때만 사용한다.

## 개발 검증

루트와 `desktop`은 별도 Go 모듈이다. `make test`, `make vet`는 두 모듈을 검증한다. PostgreSQL 테스트는 명시한 전용 DB에서 임시 스키마를 생성한다. 실제 컨테이너 로그를 검증하려면 `WHALECHIME_TEST_CONTAINER_ID`로 테스트 컨테이너를 지정한다. 해당 테스트는 `whalechime-live-stdout 정상 출력`, `whalechime-live-stderr 오류 출력`이라는 줄을 기대한다.

동일한 두 줄을 출력한 뒤 종료한 TTY 컨테이너는 `WHALECHIME_TEST_TTY_CONTAINER_ID`로 지정한다. TTY에서는 Docker가 두 출력을 합쳐 제공하므로 합쳐진 출력에 두 줄이 모두 보존되는지 확인한다.

Linux 트레이는 독립 DBus 세션에서 실제 메뉴 구조와 선택 동작을 검증한다. Windows용 트레이는 `desktop`에서 `GOOS=windows CGO_ENABLED=0 go build .`로 교차 빌드할 수 있다. macOS 실행은 해당 OS의 Go·GUI 빌드 환경에서 수행한다.
