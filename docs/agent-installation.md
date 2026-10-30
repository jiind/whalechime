# 에이전트 설치와 실행 권한

## 설치 원칙

기존 Docker 접근 권한이 있는 계정으로 실행한다. 호스트의 사용자·그룹·소켓 권한과 시스템 서비스 설정을 자동 변경하지 않는다. 설치 준비 스크립트는 Docker 연결을 확인하고 현재 계정이 소유하는 앱 전용 설정·토큰·데이터 디렉터리만 생성한다.

일반 알림 사용자와 사용자 PC의 트레이 앱에는 Docker 소켓 연결이 필요하지 않다. 이 설정은 각 Docker 서버의 에이전트 설치에만 적용된다.

## 호스트에서 직접 실행

현재 계정으로 Docker에 접근할 수 있다면 같은 계정으로 에이전트를 실행한다. Docker CLI의 별도 context를 사용하는 경우 해당 소켓 주소를 `DOCKER_HOST`로 지정한다.

```sh
export DOCKER_HOST=unix:///var/run/docker.sock
export WHALECHIME_SERVER_URL=https://whalechime.example.com
export WHALECHIME_AGENT_TOKEN_FILE=/사용자/전용/경로/agent-token
export WHALECHIME_AGENT_DATA_DIR=/사용자/전용/경로/agent-data
go run ./cmd/whalechime-agent -check
go run ./cmd/whalechime-agent
```

`-check`는 토큰 설정, 앱 데이터 디렉터리 생성·잠금·쓰기, Docker 소켓 연결을 검사하고 종료한다. 중앙 서버의 토큰 유효성을 확인하거나 이벤트 수집을 시작하지 않는다.

## 컨테이너로 실행

Linux 서버에서 Whalechime 저장소를 준비하고 다음 명령을 실행한다. 중앙 서버에 서버를 등록할 때 발급받은 에이전트 토큰을 입력한다.

```sh
bash scripts/configure-agent.sh
```

이 스크립트는 현재 Docker 연결에서 Unix 소켓을 찾는다. 일반 Docker에서는 현재 계정의 UID·기본 GID와 소켓 소유 그룹의 GID를 실행 설정에 기록한다. Rootless Docker는 현재 context의 소켓을 사용하고 기존 호스트 사용자로 매핑되는 컨테이너 UID·GID 0을 사용한다. 호스트의 관리자 계정으로 전환하는 설정이 아니다. Rootless가 아닌 `userns-remap` 환경은 소켓·앱 데이터의 ID 매핑이 다르므로 설치 준비를 중단하고 호스트 직접 실행 방식을 안내한다.

기본 생성 위치는 `.local/agent/`이며 `WHALECHIME_AGENT_SETUP_DIR`로 바꿀 수 있다. 토큰은 별도 파일에 저장하고 Compose 환경 설정에는 넣지 않는다. 새 파일은 현재 계정만 읽을 수 있도록 생성한다. 기존 설정이 있으면 덮어쓰지 않는다.

```sh
docker compose --env-file .local/agent/agent.env -f deploy/agent.compose.yaml run --build --rm agent -check
docker compose --env-file .local/agent/agent.env -f deploy/agent.compose.yaml up --build -d
```

일반 Docker에서는 에이전트가 실행 계정의 UID·GID로 동작하고 컨테이너 내부에 소켓 소유 그룹 번호를 추가한다. 호스트 계정의 그룹 구성을 바꾸지 않는다. 소켓과 앱 데이터 디렉터리는 이미 존재해야 하며 Compose가 잘못된 경로를 대신 생성하지 않는다.

Compose는 컨테이너의 특권 모드를 사용하지 않는다. 소켓의 읽기 전용 마운트는 Docker API의 작업별 권한 제한을 뜻하지 않는다. Whalechime 에이전트 코드는 이벤트 구독과 컨테이너 정보·로그 조회만 수행한다.

중앙 서버 주소는 에이전트 컨테이너에서 접속할 수 있어야 한다. 기본 브리지 네트워크에서는 `127.0.0.1`이 에이전트 컨테이너 자신을 의미하므로 중앙 서버의 접속 가능한 주소를 사용한다.

## 접근이 실패하는 경우

소켓 경로와 에이전트의 실행 UID·GID·추가 그룹을 확인한다. 현재 계정에 원래 Docker 접근 권한이 없다면 설치 준비를 중단한다. 권한이 없는 환경을 자동으로 우회하거나 호스트 소켓 권한을 완화하지 않는다.

## 로그 범위

- 컨테이너 stdout·stderr: 기존 Docker 연결을 통해 조회한다.
- Docker 변경 이벤트: 기존 Docker 연결로 구독한다.
- Docker 데몬의 호스트 원본 로그: 별도 journal·로그 파일 읽기 권한이 필요할 수 있으므로 기본 기능에 포함하지 않는다.

Docklattice 화면의 `Docker Engine-retained logs`도 Docker가 보관하는 컨테이너 출력이다. `dockerd` 자체의 시스템 로그와 구분한다.

## 검증 범위

일반 Linux Docker에서 현재 계정의 UID·GID와 소켓 그룹 번호를 적용한 컨테이너 실행 및 `-check` 통과를 확인했다. 공백·따옴표·달러 기호가 포함된 설치 경로, 비밀 파일 접근 권한, 기존 설정 덮어쓰기 차단, 호스트 소켓 권한 유지도 검증했다. Rootless Docker의 UID 매핑 분기는 실제 Rootless 데몬에서의 실행 검증이 남아 있다.
