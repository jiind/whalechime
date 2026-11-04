# 기여 안내

Whalechime은 한 사용자가 여러 Docker 서버에 직접 연결하는 초기 단계의 로컬 트레이 앱입니다. 변경 전 [요구사항](docs/requirements.md)과 [작업 지침](AGENTS.md)을 확인하세요.

## 이슈와 브랜치

버그는 재현 절차·기대 동작·실제 동작·OS·Docker 버전을, 기능 제안은 사용 상황과 필요한 동작을 적어주세요. 자격 증명과 실제 로그의 비밀값은 제거하세요. 취약점은 [비공개 제보](SECURITY.md)를 이용하세요.

이슈 제목은 영어 소문자 `fix: ...`, `feat: ...`, `docs: ...` 형식을 사용하고 본문은 한국어로 작성합니다. 기존 라벨 중 가까운 것을 사용합니다. 이슈가 있는 브랜치는 `1-feat-local-client`처럼 번호·유형·영어 소문자 케밥 표기를 사용하고 이슈의 Development에 연결합니다. 이슈가 없다면 `fix/connection-timeout`처럼 작성합니다.

## 개발과 검증

```sh
go run ./desktop
make vet
make test
make build
npm ci
npx playwright install chromium
npm run test:ui
```

설명 주석·문서는 한국어로 작성하고 Go 코드는 `gofmt`를 적용합니다. 모든 명시적 함수·메서드 위에 역할 주석을 작성하고 여러 처리 단계가 있으면 본문에 번호 주석을 추가합니다. 테스트에는 사용자 개인 설정·실제 수신처 대신 임시 데이터와 로컬 수신처를 사용합니다.

PR에는 바뀌는 사용자 동작과 검증 결과·미확인 범위를 적고 관련 이슈를 연결하세요. `main`은 PR과 `go`·`browser` 필수 검사를 사용하며 Squash로 병합합니다. 병합 후 작업 브랜치는 자동 삭제됩니다. 제출한 코드는 저장소의 [Apache License 2.0](LICENSE)에 따라 배포됩니다.
