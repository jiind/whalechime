# 구현과 검증 기록

2026-11-04의 로컬 클라이언트 전환을 기준으로 기록한다. 이전 중앙 서버·PostgreSQL·에이전트 검증 결과를 현재 구조의 검증으로 간주하지 않는다.

## 확인한 항목

| 범위 | 검증 근거 |
| --- | --- |
| HTTP 기본 연결 | 실제 SDK의 HTTP 목록 조회, 브라우저 저장·수정·새로고침·삭제 |
| HTTPS | 실제 TLS 서버·사설 CA 조회 성공, 신뢰하지 않는 CA 거부 |
| 선택적 mTLS | 실제 클라이언트 인증서 검증 성공, 인증서 없는 연결 거부 |
| 프로토콜 조합 | HTTP에 TLS 설정 혼합·주소 내 비밀번호·경로·쿼리 거부 |
| SSH 전송 | 자식 프로세스의 양방향 표준 입출력에서 실제 HTTP 요청·응답, 고정 명령·호스트 검증 옵션 검사 |
| 읽기 전용 | SDK의 컨테이너 삭제 요청이 네트워크에 도달하기 전 차단 |
| 이벤트·알림 | 직접 이벤트 스트림 → 로컬 이력 → 개인 Webhook, 중복 제거·실패 수신처 재시도·대기 중 해제 |
| 실제 Docker | 테스트 전용 루프백 HTTP → 기존 Docker 소켓에서 실제 컨테이너 생성 이벤트·한글 로그 조회 |
| 로그 수명 | 최근 200줄·실시간·시각 옵션, 서버 연결 삭제 시 실제 업스트림 조회 취소 |
| 로컬 저장 | 암호문에 자격 증명 평문 없음, 재시작 복원, 중복 실행 차단, 실패 변경의 메모리 미반영 |
| 로컬 웹 경계 | 실행별 HttpOnly 세션, 인증 없는 요청·외부 Host·외부 Origin·폼 요청 거부 |
| 브라우저 | 수신 항목·수신처 독립 저장, HTTP·HTTPS·mTLS·SSH 입력 전환, 390px 모바일 화면 |
| 트레이 | 독립 DBus 세션에서 서버 펼침·컨테이너 클릭·웹 이동·설정 이동 분리 |

## 실행 명령

```sh
gofmt -l cmd internal desktop
go vet ./...
go test -race -count=1 ./...
make build
WHALECHIME_NATIVE_TRAY_TEST=1 dbus-run-session -- go test -race -count=1 -run TestNativeTrayHierarchy -v ./desktop
GOOS=windows CGO_ENABLED=0 go build -o /tmp/whalechime.exe ./desktop
WHALECHIME_TEST_DOCKER=1 go test -race -count=1 -run TestRealDockerDirect -v ./internal/localapp
npm run test:ui
git diff --check
```

로컬 브라우저 검증은 설치된 Chrome을 `WHALECHIME_TEST_BROWSER_PATH`로 지정했다. GitHub의 `browser` 검사는 Playwright Chromium을 설치해 같은 시나리오를 실행한다. 원격 실행 결과는 PR의 필수 `go`·`browser` 검사를 따른다.

구현 커밋 `d489909`의 [GitHub Actions 실행](https://github.com/jiind/whalechime/actions/runs/41792336765)에서 `go`와 `browser`가 모두 통과했다. 이후 문서 변경이나 병합 커밋의 검사 결과는 해당 커밋의 실행 결과를 별도로 확인한다.

실제 Docker 테스트는 로컬에 `alpine:3.22`가 필요하며 테스트 소유 컨테이너만 생성·정리한다. Docker 데몬 설정이나 호스트 권한을 변경하지 않는다. 원격 데몬의 외부 API 포트를 실제로 개방한 검증과 구분한다.

## 미확인 환경

- 실제 원격 SSH 호스트의 인증·키·Docker CLI 조합. 자동 테스트는 SSH 프로세스 경계를 대체한 전송 테스트다.
- 사용자 Windows·macOS 데스크톱에서 트레이 렌더링·클릭·OS 알림. Windows는 교차 빌드, macOS는 실행 미검증이다.
- 실제 사용자의 SMTP·메신저 계정 접속과 최종 배달. 자동 검증은 로컬 수신처와 요청 계약을 사용한다.
- 실제 사용자 데스크톱의 알림 표시·방해 금지 설정, PC 절전 복귀 시 운영 환경별 네트워크 동작.
