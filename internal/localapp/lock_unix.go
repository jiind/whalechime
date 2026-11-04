//go:build !windows

package localapp

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"path/filepath"
)

// lockDirectory는 앱 종료 시 자동 해제되는 프로세스 잠금을 획득한다.
func lockDirectory(dir string) (*os.File, error) {
	file, err := os.OpenFile(filepath.Join(dir, ".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		return nil, errors.New("이 데이터 디렉터리에서 Whalechime이 이미 실행 중입니다")
	}
	return file, nil
}
