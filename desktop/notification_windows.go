package main

import (
	"context"
	"errors"
	"github.com/jiind/whalechime/internal/notify"
	"os"
	"os/exec"
)

// notifyDesktop는 고정된 PowerShell 코드에 환경변수로만 알림 값을 전달한다.
func notifyDesktop(ctx context.Context, m notify.Message) error {
	// 1. 메시지를 코드 문자열에 삽입하지 않고 자식 프로세스의 값으로 전달한다.
	script := `Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; $notice=New-Object System.Windows.Forms.NotifyIcon; try { $notice.Icon=[System.Drawing.SystemIcons]::Information; $notice.Visible=$true; $notice.ShowBalloonTip(3000,$env:WHALECHIME_NOTICE_TITLE,$env:WHALECHIME_NOTICE_BODY,[System.Windows.Forms.ToolTipIcon]::Info); Start-Sleep -Seconds 3 } finally { $notice.Dispose() }`
	command := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	command.Env = append(os.Environ(), "WHALECHIME_NOTICE_TITLE="+m.Title, "WHALECHIME_NOTICE_BODY="+m.Body)

	// 2. 표시 실패를 자격 증명 없는 오류로 반환해 발송 이력에 남긴다.
	if err := command.Run(); err != nil {
		return errors.New("PC 알림을 표시할 수 없습니다")
	}
	return nil
}
