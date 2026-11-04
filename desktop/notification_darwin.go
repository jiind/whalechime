package main

import (
	"context"
	"errors"
	"github.com/jiind/whalechime/internal/notify"
	"os/exec"
)

// notifyDesktop는 macOS 알림 센터에 제목과 본문을 데이터 인자로 전달한다.
func notifyDesktop(ctx context.Context, m notify.Message) error {
	script := "on run argv\ndisplay notification (item 2 of argv) with title (item 1 of argv)\nend run"
	if err := exec.CommandContext(ctx, "osascript", "-e", script, "--", m.Title, m.Body).Run(); err != nil {
		return errors.New("PC 알림을 표시할 수 없습니다")
	}
	return nil
}
