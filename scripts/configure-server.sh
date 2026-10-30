#!/usr/bin/env bash
# 앱 전용 파일에 초기 관리자 토큰·DB 암호·수신처 암호화 키를 생성한다.
set -euo pipefail
set -C
umask 077

# fail은 원문 자격 증명을 노출하지 않고 준비를 중단한다.
fail() {
  printf '%s\n' "$1" >&2
  exit 1
}

# 1. 기존 설정을 덮어쓰지 않고 암호학적 난수 생성 도구를 확인한다.
command -v openssl >/dev/null || fail 'OpenSSL이 필요합니다.'
repository_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
setup_dir="${WHALECHIME_SERVER_SETUP_DIR:-$repository_dir/.local/server}"
mkdir -p -- "$setup_dir"
[[ ! -e "$setup_dir/server.env" && ! -L "$setup_dir/server.env" ]] || fail '기존 서버 설정을 덮어쓰지 않았습니다.'

# 2. 호스트나 계정 권한을 바꾸지 않고 앱 자격 증명을 저장한다.
{
  printf 'POSTGRES_PASSWORD=%s\n' "$(openssl rand -hex 32)"
  printf 'WHALECHIME_ADMIN_TOKEN=wc_admin_%s\n' "$(openssl rand -hex 32)"
  printf 'WHALECHIME_ENCRYPTION_KEY=%s\n' "$(openssl rand -base64 32)"
  printf 'WHALECHIME_BIND_ADDRESS=127.0.0.1\nWHALECHIME_PORT=8080\n'
  printf 'WHALECHIME_ALLOWED_NOTIFICATION_HOSTS=\n'
} > "$setup_dir/server.env"

# 3. 비밀값을 출력하지 않고 실행 방법만 안내한다.
printf '서버 설정을 생성했습니다: %s/server.env\n' "$setup_dir"
printf 'docker compose --env-file %q -f %q up --build -d\n' "$setup_dir/server.env" "$repository_dir/deploy/server.compose.yaml"
