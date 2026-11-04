# Whalechime

여러 Docker 서버의 변화와 컨테이너 로그를 내 PC에서 확인하는 개인용 트레이 앱입니다.

**초기 공개 버전입니다.** 앱이 실행 중이고 서버에 연결되어 있을 때만 수집·알림이 동작합니다. PC 종료·절전·네트워크 단절 중의 완전한 이력 복구는 보장하지 않습니다.

- **직접 연결:** 서버별 HTTP·HTTPS Docker API, 선택적 mTLS, SSH
- **트레이:** 우클릭 → Docker 서버 → 컨테이너 하위 메뉴 → 즉시 로그 화면
- **로컬 설정:** 로그인 없이 서버 연결·수신 항목·수신처 관리
- **알림:** 이 PC의 데스크톱 알림과 선택적인 외부 메신저·메일
- **로컬 보관:** 연결 설정·수집 이력·발송 대기를 사용자 전용 암호화 파일에 저장
- 중앙 서버·DB·에이전트 설치가 필요하지 않습니다. Docker 상태를 변경하는 기능은 없습니다.

## 실행

Go 1.25 이상과 데스크톱 트레이 환경을 준비합니다.

```sh
go run ./desktop
```

트레이의 **설정 → 서버 연결 설정**에서 서버를 추가합니다.

| 연결 | 입력 |
| --- | --- |
| HTTP | `http://192.168.1.10:2375` |
| HTTPS | `https://docker.example:2376`, 필요하면 CA 인증서 경로 |
| mTLS | HTTPS에서 mTLS를 켜고 클라이언트 인증서·개인키 경로 지정 |
| SSH | `ssh://사용자@서버:22` 또는 `ssh://SSH별칭`, 필요하면 개인키 경로 |

Docker API에 이미 접근할 수 있어야 합니다. 앱은 서버의 Docker 데몬·방화벽·계정 권한을 자동 변경하지 않습니다. HTTP는 통신을 암호화하지 않습니다. 인증 없는 Docker API는 접근 가능한 클라이언트에 제어 권한도 줄 수 있으므로 서버의 접근 허용 범위를 직접 관리하세요. HTTPS는 서버 인증서를 항상 검증하며 SSH는 기존 `known_hosts`를 사용합니다. [Docker 연결 안내](https://docs.docker.com/engine/security/protect-access/)

처음에는 모든 이벤트 알림이 꺼져 있습니다. **수신 항목**에서 필요한 항목을 켜면 기본 수신처인 **이 PC 알림**으로 받습니다. 외부 채널은 **내 수신처**에서 추가합니다. 수신 설정을 꺼도 이벤트 수집·기록은 계속됩니다.

## 트레이와 로그

서버 하위 메뉴를 펼치는 동안에는 창을 열지 않습니다. 컨테이너를 클릭하면 최근 200줄과 이후 stdout·stderr가 시각과 함께 로그 전용 화면에 자동 표시됩니다. **웹으로 이동**은 로컬 대시보드, **설정**은 로컬 설정 화면을 엽니다. Docker 데몬의 호스트 원본 로그는 기본 조회 범위에 포함하지 않습니다.

- [트레이와 로그 보기](docs/tray.md)
- [서버 연결 설정](docs/connections.md)
- [알림 채널](docs/notifications.md)
- [데이터 보관과 운영](docs/operations.md)

## 빌드와 검증

```sh
make build
make vet
make test
npm ci
npx playwright install chromium
npm run test:ui
```

`bin/whalechime`은 트레이 앱입니다. 브라우저 테스트는 사용자 설정과 분리된 임시 로컬 앱과 로컬 HTTP 수신처를 자동 생성하고 정리합니다. 외부 계정으로 알림을 보내지 않습니다.

Linux에서는 StatusNotifier/AppIndicator 트레이와 데스크톱 알림 서비스가 필요합니다. Windows는 교차 빌드를 검증하며 실제 알림·클릭 동작은 해당 OS에서 추가 확인이 필요합니다. macOS 실제 실행은 아직 검증하지 않았습니다. [검증 범위](docs/verification.md)

## 동작의 경계

- 수집한 최근 이벤트 2,000개와 발송 대기를 PC에 저장합니다. Docker 자체의 과거 이벤트 조회는 최근 256개로 제한됩니다. [Docker 이벤트 문서](https://docs.docker.com/reference/cli/docker/system/events/)
- 선택한 항목은 모든 활성 수신처에 전달합니다. 실패한 수신처만 최대 8회 시도합니다. 외부 서비스 접수 직후 앱이 종료되거나 상태 저장이 실패하면 중복 알림이 생길 수 있습니다.
- SSH에는 로컬 OpenSSH 클라이언트와 원격 서버의 Docker CLI·소켓 접근 권한이 필요합니다. 비밀번호 입력과 새 호스트 자동 신뢰는 지원하지 않습니다.
- 로컬 암호화 키도 같은 PC에 있습니다. 암호화는 파일의 평문 노출을 줄이지만 동일 사용자 권한이나 PC 전체 탈취를 방어하는 보장은 아닙니다.
- 팀 공유·중앙 수집·PC가 꺼져 있는 동안의 감시는 제공하지 않습니다.

기여 방법은 [CONTRIBUTING.md](CONTRIBUTING.md), 비공개 취약점 제보는 [SECURITY.md](SECURITY.md), 제품 범위는 [요구사항](docs/requirements.md)을 참고하세요.

## 라이선스

[Apache License 2.0](LICENSE)
