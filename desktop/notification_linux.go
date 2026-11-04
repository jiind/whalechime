package main

import (
	"context"
	"errors"
	"github.com/godbus/dbus/v5"
	"github.com/jiind/whalechime/internal/notify"
)

// notifyDesktop는 표준 데스크톱 DBus 알림으로 서버 이벤트를 표시한다.
func notifyDesktop(ctx context.Context, m notify.Message) error {
	// 1. 현재 사용자의 데스크톱 알림 서비스에 연결한다.
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		return errors.New("데스크톱 알림 서비스에 연결할 수 없습니다")
	}
	defer bus.Close()

	// 2. 외부 문자열을 코드로 해석하지 않고 제목·본문 값으로 전달한다.
	call := bus.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").CallWithContext(ctx, "org.freedesktop.Notifications.Notify", 0, "Whalechime", uint32(0), "dialog-information", m.Title, m.Body, []string{}, map[string]dbus.Variant{}, int32(5000))
	if call.Err != nil {
		return errors.New("PC 알림을 표시할 수 없습니다")
	}
	return nil
}
