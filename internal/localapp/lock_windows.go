package localapp

import (
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
)

// lockDirectory는 Windows 파일 잠금으로 같은 데이터의 동시 수정을 차단한다.
func lockDirectory(dir string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		file.Close()
		return nil, errors.New("이 데이터 디렉터리에서 Whalechime이 이미 실행 중입니다")
	}
	return file, nil
}
