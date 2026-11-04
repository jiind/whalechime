# 로컬 앱 API

이 API는 PC 루프백에서 트레이와 내장 웹 화면이 사용하는 내부 계약이다. 중앙 서버용 공개 API나 Docker를 대신하는 네트워크 프록시가 아니다. 실행별 자격 증명이 필요하며 외부 호스트·사이트의 요청을 허용하지 않는다.

| 경로 | 용도 |
| --- | --- |
| `GET/POST /api/servers` | 서버 목록·연결 추가 |
| `PUT/DELETE /api/servers/{id}` | 연결 수정·삭제 |
| `POST /api/servers/{id}/test` | Docker 연결 확인 |
| `GET /api/servers/{id}/containers` | 컨테이너 목록 |
| `GET /api/servers/{id}/containers/{container}/logs` | 최근 로그·실시간 출력 |
| `GET/PUT /api/me/preferences` | 이벤트 수신 항목 |
| `GET/POST /api/me/destinations` | 수신처 목록·추가 |
| `PUT/PATCH/DELETE /api/me/destinations/{id}` | 수신처 수정·활성화·삭제 |
| `POST /api/me/destinations/{id}/test` | 명시적 테스트 알림 |
| `GET /api/events` | 서버·항목별 최근 이력 |
| `GET /api/me/deliveries` | 최근 발송 결과 |
| `GET /api/event-types` | 알려진 항목과 관찰한 새 항목 |
| `GET /api/destination-types` | 지원하는 수신처 종류 |

변경 요청은 JSON 본문을 사용한다. 이벤트와 발송 이력 조회는 최대 500개를 반환한다. 로그는 줄 단위 JSON 프레임으로 전송하며 `data`는 원본 바이트의 Base64다. 수신처 조회·저장 응답은 연결 URL·토큰·비밀번호를 반환하지 않는다.

Docker에는 별도로 `/_ping`, `/version`, `/events`, `/containers/json`, `/containers/{id}/json`, `/containers/{id}/logs`의 조회 요청만 전송한다. API 버전은 SDK가 협상한다.
