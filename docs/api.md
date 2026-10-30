# API 계약

사용자 API는 `Authorization: Bearer <사용자 토큰>`, 에이전트 API는 별도로 발급한 서버 전용 토큰을 사용한다. 본인 설정 경로는 요청 본문의 사용자 식별자를 받지 않는다. 목록의 기본 조회 수는 100개, 최대 500개다.

## 개인 API

| 메서드·경로 | 내용 |
| --- | --- |
| `GET /api/me` | 내 계정 |
| `GET /api/servers` | 직접 또는 팀을 통해 접근 가능한 서버 |
| `GET /api/events` | 접근 가능한 서버의 이벤트. `server_id`, `event_key`, `limit` 필터 |
| `GET /api/event-types` | 기본 목록과 접근 가능한 서버에서 관찰한 새 이벤트 |
| `GET /api/destination-types` | 현재 지원하는 채널 목록 |
| `GET /api/me/preferences` | 내 수신 항목 |
| `PUT /api/me/preferences` | 내 수신 항목 전체 교체 |
| `GET /api/me/destinations` | 내 수신처. 연결 비밀값은 반환하지 않음 |
| `POST /api/me/destinations` | 내 수신처 추가 |
| `PUT /api/me/destinations/{id}` | 연결 정보를 포함해 내 수신처 전체 변경 |
| `PATCH /api/me/destinations/{id}` | `enabled`만 변경 |
| `DELETE /api/me/destinations/{id}` | 내 수신처 삭제 |
| `POST /api/me/destinations/{id}/test` | 내 수신처에 명시적으로 테스트 발송 |
| `GET /api/me/deliveries` | 내 발송 이력. `limit` 지원 |

수신 항목 예:

```json
{
  "default_enabled": false,
  "items": [
    { "event_key": "image.pull", "enabled": true },
    { "event_key": "image.push", "enabled": true },
    { "event_key": "container.die", "enabled": true }
  ]
}
```

`default_enabled`는 목록에 없는 항목과 향후 발견한 이벤트의 기본값이다. 이벤트 키는 `객체종류.동작`이다. `health_status: healthy`, `exec_create: ...`처럼 상태나 명령을 포함하는 동작은 각각 `container.health_status`, `container.exec_create`로 정규화하며 전체 동작은 이벤트 상세에 보존한다.

수신처 예:

```json
{
  "name": "개인 알림",
  "kind": "webhook",
  "enabled": true,
  "config": { "url": "https://notifications.example.com/docker", "token": "선택적 Bearer 토큰" }
}
```

## 관리자 API

| 메서드·경로 | 내용 |
| --- | --- |
| `GET`, `POST /api/admin/users` | 사용자 조회·등록. 등록 응답에서만 토큰 원문 반환 |
| `POST /api/admin/users/{user}/token` | 일반 사용자 토큰 재발급. 이전 토큰 폐기 |
| `GET`, `POST /api/admin/servers` | 서버 조회·등록. 등록 응답에서만 에이전트 토큰 반환 |
| `GET /api/admin/servers/{server}/members` | 직접 연결한 사용자 |
| `PUT /api/admin/servers/{server}/members/{user}` | 직접 사용자 연결. 본문 `{"enabled":true}` |
| `GET`, `POST /api/admin/teams` | 팀 조회·등록 |
| `DELETE /api/admin/teams/{team}` | 팀과 팀 연결 삭제 |
| `GET /api/admin/teams/{team}/members` | 팀 구성원 |
| `PUT /api/admin/teams/{team}/members/{user}` | 팀 구성원 변경. 본문 `{"enabled":true}` |
| `GET /api/admin/servers/{server}/teams` | 서버 담당 팀 |
| `PUT /api/admin/servers/{server}/teams/{team}` | 서버 담당 팀 변경. 본문 `{"enabled":true}` |

사용자·서버·팀 등록 본문은 `{"name":"표시 이름"}`이다. 서버 등록자는 기본 관련 사용자로 직접 연결된다. 관리자도 다른 사용자의 개인 수신처를 조회·수정하지 않는다. 직접 연결과 팀 연결이 겹쳐도 한 수신처에는 발송 대기 하나만 생성한다.

## 에이전트 API

`POST /api/agent/events`는 서버 식별자를 본문에서 받지 않고 인증 토큰으로 결정한다.

```json
{
  "type": "image",
  "action": "push",
  "actor_id": "example/app:1.0",
  "scope": "local",
  "time_nano": 1793200000000000000,
  "attributes": { "name": "example/app:1.0" }
}
```

응답은 DB 저장 후 `202 {"id":"이벤트 해시","created":true}`다. 같은 이벤트 재전송은 `created:false`가 된다. `POST /api/agent/heartbeat`는 `204`로 응답한다.

읽기 전용 컨테이너 조회·로그 스트림 API는 [트레이 문서](tray.md)의 계약을 따른다. 이미지·컨테이너를 변경하는 API는 없다.
