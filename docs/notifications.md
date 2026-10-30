# 개인 수신처 연동

수신 항목은 **수신 항목** 화면에서, 알림이 도착할 위치는 **내 수신처**에서 관리한다. 이벤트마다 채널을 다르게 지정하지 않는다. 항목을 켜면 본인의 활성 수신처 전체로 같은 이벤트를 발송한다.

수신처를 저장한 뒤 **테스트 발송**으로 연결을 확인할 수 있다. 테스트 발송은 해당 사용자가 명시적으로 요청한 동작이므로 비활성 수신처에서도 가능하다. 설정 수정 시 기존 연결 정보는 조회하지 않으며 필요한 값을 다시 입력한다.

| 종류 | 연결 정보 | 참고 |
| --- | --- | --- |
| 이메일 | SMTP 호스트·포트·사용자·비밀번호·발신/수신 주소 | 465는 TLS, 다른 포트는 STARTTLS를 사용한다. 인증 없는 TLS SMTP도 가능하다. |
| Slack | Incoming Webhook URL | 해당 대화에 앱을 설치하고 발급한 주소를 사용한다. |
| Teams | Workflows Webhook URL | `When a Teams webhook request is received`를 사용해 원하는 채팅 또는 채널에 연결한다. |
| Discord | 채널 Webhook URL | 자동 멘션은 비활성화한다. |
| Telegram | Bot 토큰, chat ID | 수신 사용자가 봇과 대화를 시작하거나 봇을 해당 그룹에 추가해야 한다. |
| Google Chat | 스페이스 Webhook URL | 조직에서 Webhook 사용을 허용해야 한다. |
| Mattermost | Incoming Webhook URL | 자체 운영 서버도 사용할 수 있다. |
| ntfy | 토픽 URL, 선택적 Bearer 토큰 | 예: `https://ntfy.example.com/docker-notifications` |
| Gotify | 서버 기본 URL, 애플리케이션 토큰 | 서버의 `/message` API에 전송한다. |
| Pushover | 애플리케이션 토큰, 사용자 키 | 서비스의 메시지 API에 전송한다. |
| 사용자 정의 Webhook | HTTP(S) URL, 선택적 Bearer 토큰 | `title`, `message`를 담은 JSON 객체로 POST한다. |

SMS·브라우저 푸시 등 별도 제공자 연동은 새 발송 모듈로 확장할 수 있다. 현재 지원 목록은 `GET /api/destination-types`가 반환하는 11개 채널이며 아직 구현하지 않은 채널을 선택지로 노출하지 않는다.

## 내부 연동 주소

기본적으로 HTTPS 공개 주소로 발송한다. 사설 Mattermost·Gotify·SMTP·Webhook 등은 중앙 서버의 `WHALECHIME_ALLOWED_NOTIFICATION_HOSTS`에 허용할 호스트 이름 또는 IP를 쉼표로 지정한다. 예외는 호스트 단위이며 HTTP도 허용되므로 실제로 신뢰하는 연동 주소만 넣는다.

DNS 조회 후 실제 연결할 IP도 검사하고 리다이렉트는 따라가지 않는다. HTTP 응답 본문이나 자격 증명이 포함될 수 있는 URL을 오류 이력에 저장하지 않는다.

## 발송 결과

발송 이력의 성공은 외부 서비스가 요청을 정상 접수했음을 뜻한다. 실제 사용자의 읽음이나 이메일 최종 배달 완료를 뜻하지 않는다. 실패한 수신처만 지수 대기 후 최대 8회 시도한다. 재시도 전에 해당 사용자의 현재 수신 항목·수신처·서버 연결 권한을 다시 확인한다.

실제 외부 계정은 사용자가 등록한 뒤 테스트 발송으로 확인한다. 자동 검증은 전용 로컬 HTTP 수신처와 서비스별 요청 형식을 검증하며 실제 사용자나 조직으로 메시지를 보내지 않는다.
