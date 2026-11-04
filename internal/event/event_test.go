package event

import "testing"

// TestIdentity는 속성 순서와 서버 경계가 중복 판단에 올바르게 반영되는지 확인한다.
func TestIdentity(t *testing.T) {
	// 1. 속성 순서만 다른 동일 이벤트를 준비한다.
	a := Event{Type: "image", Action: "push", ActorID: "sha256:abc", TimeNano: 1000, Attributes: map[string]string{"name": "app:v1", "other": "value"}}
	b := Event{Type: "image", Action: "push", ActorID: "sha256:abc", TimeNano: 1000, Attributes: map[string]string{"other": "value", "name": "app:v1"}}
	if a.ID("server-a") != b.ID("server-a") {
		t.Fatal("속성 순서가 달라도 같은 이벤트여야 합니다")
	}

	// 2. 다른 서버의 같은 이벤트와 명령어를 구분한다.
	if a.ID("server-a") == a.ID("server-b") {
		t.Fatal("서버가 다르면 별개 이벤트여야 합니다")
	}
	for action, expected := range map[string]string{"health_status: healthy": "container.health_status", "exec_create: sh -c echo secret": "container.exec_create", "start": "container.start"} {
		if key := (Event{Type: "container", Action: action}).Key(); key != expected {
			t.Fatalf("정규화 오류: %s != %s", key, expected)
		}
	}
}
