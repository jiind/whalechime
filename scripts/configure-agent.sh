#!/usr/bin/env bash
# 기존 Docker 접근 권한을 확인하고 사용자 소유의 에이전트 실행 설정만 생성한다.
set -euo pipefail
set -C
umask 077

# fail은 호스트 권한을 자동 변경하지 않고 설치 준비를 중단한다.
fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

# write_setting은 Compose가 값을 재해석하지 않도록 작은따옴표로 감싼다.
write_setting() {
  local key="$1"
  local value="$2"
  value="${value//\'/\\\'}"
  printf "%s='%s'\n" "$key" "$value"
}

# 1. 기존 Docker 연결이 사용 가능한지 확인한다.
[[ "$(uname -s)" == Linux ]] || fail '컨테이너 에이전트 설치 준비는 Linux에서 실행하세요.'
command -v docker >/dev/null || fail 'Docker 실행 파일을 찾을 수 없습니다.'
docker compose version >/dev/null || fail 'Docker Compose 플러그인이 필요합니다.'
docker_security="$(docker info --format '{{json .SecurityOptions}}')" || fail '현재 계정의 기존 Docker 연결을 확인하세요. 시스템 권한은 변경하지 않았습니다.'

if [[ -n "${DOCKER_CONTEXT:-}" ]]; then
  docker_endpoint="$(docker context inspect --format '{{.Endpoints.docker.Host}}' "$DOCKER_CONTEXT")"
elif [[ -n "${DOCKER_HOST:-}" ]]; then
  docker_endpoint="$DOCKER_HOST"
else
  docker_endpoint="$(docker context inspect --format '{{.Endpoints.docker.Host}}')"
fi
[[ "$docker_endpoint" == unix:///* ]] || fail '이 설치 구성은 같은 Linux 서버의 Unix Docker 소켓을 사용합니다.'
docker_socket="${docker_endpoint#unix://}"
[[ -S "$docker_socket" ]] || fail 'Docker 소켓 경로를 확인하세요.'
docker_gid="$(stat -Lc '%g' -- "$docker_socket")"
agent_uid="$(id -u)"
agent_gid="$(id -g)"
if [[ "$docker_security" == *'name=rootless'* ]]; then
  # Rootless의 컨테이너 UID 0은 기존 호스트 실행 사용자로 매핑된다.
  agent_uid=0
  agent_gid=0
  docker_gid=0
elif [[ "$docker_security" == *'name=userns'* ]]; then
  fail 'userns-remap 환경은 기존 계정으로 호스트에서 직접 실행하는 설치 방법을 사용하세요.'
fi

# 2. 중앙 서버 주소와 에이전트 토큰을 입력받는다.
server_url="${WHALECHIME_SERVER_URL:-}"
if [[ -z "$server_url" ]]; then
  read -r -p '중앙 서버 주소: ' server_url
fi
[[ "$server_url" =~ ^https?://[^[:space:]\#\?@]+$ ]] || fail '인증정보·쿼리·공백이 없는 HTTP(S) 중앙 서버 주소를 지정하세요.'
agent_token="${WHALECHIME_AGENT_TOKEN:-}"
if [[ -z "$agent_token" ]]; then
  if [[ -t 0 ]]; then
    read -r -s -p '서버 등록 시 발급한 에이전트 토큰: ' agent_token
    printf '\n'
  else
    IFS= read -r agent_token
  fi
fi
[[ -n "$agent_token" && "$agent_token" != *$'\n'* && "$agent_token" != *$'\r'* ]] || fail '에이전트 토큰을 확인하세요.'

# 3. 호스트 계정·그룹·소켓 권한은 그대로 두고 앱 전용 파일만 생성한다.
repository_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
setup_dir="${WHALECHIME_AGENT_SETUP_DIR:-$repository_dir/.local/agent}"
mkdir -p -- "$setup_dir"
setup_dir="$(cd -- "$setup_dir" && pwd)"
[[ ! -e "$setup_dir/agent.env" && ! -L "$setup_dir/agent.env" && ! -e "$setup_dir/agent-token" && ! -L "$setup_dir/agent-token" ]] || fail '이미 준비된 설정입니다. 기존 파일을 덮어쓰지 않았습니다.'
mkdir -p -- "$setup_dir/data"
printf '%s\n' "$agent_token" > "$setup_dir/agent-token"
{
  write_setting WHALECHIME_AGENT_UID "$agent_uid"
  write_setting WHALECHIME_AGENT_GID "$agent_gid"
  write_setting WHALECHIME_DOCKER_GID "$docker_gid"
  write_setting WHALECHIME_DOCKER_SOCKET "$docker_socket"
  write_setting WHALECHIME_SERVER_URL "$server_url"
  write_setting WHALECHIME_AGENT_TOKEN_PATH "$setup_dir/agent-token"
  write_setting WHALECHIME_AGENT_DATA_DIR "$setup_dir/data"
} > "$setup_dir/agent.env"
unset agent_token

# 4. 토큰 원문 없이 사용자가 실행할 명령만 안내한다.
printf '실행 설정을 생성했습니다. 호스트 시스템 권한은 변경하지 않았습니다.\n'
printf 'docker compose --env-file %q -f %q run --build --rm agent -check\n' "$setup_dir/agent.env" "$repository_dir/deploy/agent.compose.yaml"
printf 'docker compose --env-file %q -f %q up --build -d\n' "$setup_dir/agent.env" "$repository_dir/deploy/agent.compose.yaml"
