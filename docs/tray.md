# 트레이와 컨테이너 로그 보기

## 사용 흐름

1. 사용자 PC에서 Whalechime 트레이 앱을 실행한다.
2. 트레이 아이콘을 우클릭하면 사용자에게 연결된 Docker 서버 목록이 표시된다.
3. 서버 항목을 펼치면 옆으로 해당 서버의 컨테이너 하위 메뉴가 열린다. 이 과정에서는 웹이나 별도 창을 열지 않는다.
4. 특정 컨테이너를 클릭하면 해당 컨테이너의 로그 전용 웹 창을 기본 브라우저로 연다.
5. 로그 화면은 창 내부 전체 영역을 사용하고 최근 200줄과 이후 stdout·stderr 출력을 자동 표시한다. 추가 메뉴·대상 선택·옵션·조회 버튼이 없다.
6. `웹으로 이동`은 별도 트레이 항목으로 중앙 웹을 연다.

컨테이너와 이미지의 상태를 제어하는 기능은 제공하지 않는다. 중지된 컨테이너도 Docker에서 조회 가능한 최근 로그는 볼 수 있다. 트레이 앱을 종료해도 중앙 알림 처리는 계속된다.

## 바로 확인하기

아래 미리보기는 두 서버와 컨테이너 하위 메뉴를 실제 OS 트레이로 표시한다. 출력은 실제 Docker 로그가 아닌, 매 줄에 `미리보기`라고 표시한 합성 데이터다.

```sh
cd desktop
go run . -demo
```

Windows에서는 다음과 같이 빌드한다.

```powershell
cd desktop
go build -ldflags "-H=windowsgui" -o whalechime-tray.exe .
.\whalechime-tray.exe -demo
```

Linux에서는 StatusNotifier/AppIndicator를 지원하는 데스크톱 트레이가 필요하다. 그래픽 세션이 없는 서버에서 앱을 실행해도 사용자가 볼 트레이는 생성되지 않는다. 트레이 앱은 사용자 PC에 설치하고 Docker 서버에는 수집 에이전트만 실행한다.

## 실제 중앙 서버에 연결하기

사용자 계정의 API 토큰을 사용한다. Docker 서버용 에이전트 토큰과 구분한다.

```sh
export WHALECHIME_SERVER_URL=https://whalechime.example.com
export WHALECHIME_USER_TOKEN='<사용자 토큰>'
cd desktop
go run .
```

서버·컨테이너 목록은 15초마다 자동 갱신한다. 조회되지 않는 서버는 빈 컨테이너 목록으로 숨기지 않고 연결 오류를 표시한다. `웹으로 이동`의 브라우저 인증은 중앙 웹의 로그인 방식을 사용한다.

로그 화면은 트레이 앱이 실행한 루프백 서버의 무작위 경로로 열린다. 중앙 서버 사용자 토큰은 브라우저 주소·HTML에 전달하지 않는다. 화면이 닫히면 중앙 로그 연결과 에이전트의 Docker 로그 조회도 정리한다.

## 읽기 전용 중계

- 중앙 서버: 사용자 인증과 서버 연결 권한을 확인한다.
- 에이전트: 먼저 중앙 서버에 연결해 조회 작업을 가져온다. 별도 수신 포트를 열지 않는다.
- Docker 호출: 컨테이너 목록, 컨테이너 설정 조회, 컨테이너 로그 읽기만 허용한다.
- 로그: Docker TTY와 멀티플렉스 출력 형식을 구분하고 stdout·stderr 및 UTF-8 문자를 유지한다.
- 동시 조회: 중앙 서버는 최대 128개 작업, 에이전트는 최대 8개 작업을 처리한다.
- 권한 변경: 새 조회는 즉시 확인하고 진행 중인 로그 조회는 10초마다 서버 연결 권한을 다시 확인한다.
- 현재 중계 작업은 중앙 서버 프로세스 메모리에 있으며 다중 중앙 서버 인스턴스 간에는 공유하지 않는다. 현재 버전은 중앙 서버 한 인스턴스로 실행한다.
- 로그는 화면 조회 중에 중계하며 PostgreSQL에 컨테이너 출력 전체를 수집·보관하지 않는다.

## API

| 경로 | 인증 | 용도 |
| --- | --- | --- |
| `GET /api/servers/{server}/containers` | 사용자 | 관련 서버의 컨테이너 목록 |
| `GET /api/servers/{server}/containers/{container}/logs` | 사용자 | 선택한 컨테이너 로그 스트림 |
| `GET /api/agent/inspection/tasks` | 에이전트 | 자기 서버의 조회 작업 수신 |
| `POST /api/agent/inspection/tasks/{task}/frames` | 에이전트 | 자기 서버 작업의 응답 전송 |

로그 스트림은 줄 단위 JSON이다. 로그 프레임의 `data`는 UTF-8이 전송 조각 사이에서 손상되지 않도록 원본 바이트를 Base64로 표현한다.

## 검증

```sh
go test -race ./internal/inspection
cd desktop
go test -race ./...
```

Linux에서 OS가 실제로 사용하는 DBus 메뉴 트리와 클릭 동작을 검증할 수 있다.

```sh
cd desktop
WHALECHIME_NATIVE_TRAY_TEST=1 dbus-run-session -- go test -race -run TestNativeTrayHierarchy -v
```

이 검증은 독립 DBus 세션에서 서버 하위 컨테이너 목록, 펼침 시 화면 이동 없음, 컨테이너 클릭 시 로그 화면 이동, 별도 웹 이동을 확인한다. 실제 데스크톱의 마우스 우클릭·렌더링 검증과는 구분한다.
