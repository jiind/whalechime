package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"image"
	"image/color"
	"image/png"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"fyne.io/systray"
)

// openBrowser는 실제 메뉴 선택 시 OS 브라우저를 실행하는 경계다.
var openBrowser = openURL

// main은 실제 OS 트레이 메뉴와 로컬 로그 전용 화면을 실행한다.
func main() {
	// 1. 중앙 서버 연결 또는 명시적으로 선택한 미리보기를 준비한다.
	demo := flag.Bool("demo", false, "실제 Docker 대신 명시적인 미리보기 데이터 사용")
	flag.Parse()
	base, token := os.Getenv("WHALECHIME_SERVER_URL"), os.Getenv("WHALECHIME_USER_TOKEN")
	if *demo {
		var stop func()
		base, stop = startDemo()
		defer stop()
		token = "preview-only"
	}
	c, err := newConnection(base, token)
	if err != nil {
		slog.Error("트레이 초기화 실패", "error", err)
		os.Exit(1)
	}
	v, server, listener, err := newViewer(c)
	if err != nil {
		slog.Error("로그 화면 서버를 시작할 수 없습니다")
		os.Exit(1)
	}
	go server.Serve(listener)
	defer stopServer(server)
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	var work sync.WaitGroup

	// 2. 서버·컨테이너 선택은 트레이 안에서만 수행한다.
	systray.Run(func() {
		systray.SetIcon(trayIcon())
		systray.SetTooltip("Whalechime · Docker 변경 알림과 컨테이너 로그")
		if *demo {
			systray.SetTooltip("Whalechime · 트레이 미리보기")
		}
		work.Go(func() {
			refresh(ctx, c, v)
		})
		work.Go(func() {
			<-ctx.Done()
			systray.Quit()
		})
	}, func() {
		cancel()
	})

	// 3. 앱 종료 시 진행 중인 조회와 로컬 로그 연결을 정리한다.
	cancel()
	work.Wait()
	c.http.CloseIdleConnections()
}

// refresh는 선택 메뉴를 자동 갱신하고 화면 이동은 실제 선택 때만 수행한다.
func refresh(ctx context.Context, c *connection, v *viewer) {
	// 1. 최초 네트워크 조회 전에도 웹 이동과 종료 메뉴를 표시한다.
	stopMenu := activateMenu(ctx, c, v, nil, "서버 목록을 가져오는 중…")
	defer func() {
		stopMenu()
	}()
	var last string

	// 2. 목록이 바뀐 경우에만 메뉴를 다시 구성한다.
	for ctx.Err() == nil {
		queryCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		servers, err := c.inventory(queryCtx)
		cancel()
		message := ""
		if err != nil {
			message = err.Error()
		}
		snapshot, _ := json.Marshal(servers)
		signature := string(snapshot) + message
		if signature != last {
			stopMenu()
			stopMenu = activateMenu(ctx, c, v, servers, message)
			last = signature
		}
		timer := time.NewTimer(15 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// activateMenu는 메뉴 항목의 대기 수명을 만들고 정리 함수를 호출자에게 돌려준다.
func activateMenu(ctx context.Context, c *connection, v *viewer, servers []server, message string) context.CancelFunc {
	menuCtx, cancel := context.WithCancel(ctx)
	render(menuCtx, c, v, servers, message)
	return cancel
}

// render는 서버별 옆으로 펼쳐지는 하위 메뉴와 별도 웹 이동 항목을 만든다.
func render(ctx context.Context, c *connection, v *viewer, servers []server, message string) {
	// 1. 서버를 선택하는 단계에서는 어떤 창도 열지 않는다.
	systray.ResetMenu()
	label := systray.AddMenuItem("Whalechime", "")
	label.Disable()
	systray.AddSeparator()
	for _, host := range servers {
		parent := systray.AddMenuItem(host.Name, "컨테이너 목록 펼치기")
		if host.Unavailable {
			parent.AddSubMenuItem("서버에 연결할 수 없습니다", "").Disable()
			continue
		}
		if len(host.Containers) == 0 {
			parent.AddSubMenuItem("컨테이너가 없습니다", "").Disable()
		}
		for _, item := range host.Containers {
			leaf := parent.AddSubMenuItem(item.Name+" · "+item.State, "이 컨테이너 로그 열기")
			selected := target{ServerID: host.ID, ServerName: host.Name, ContainerID: item.ID, ContainerName: item.Name}
			watch(ctx, leaf, func() {
				openBrowser(v.open(selected))
			})
		}
	}
	if message != "" {
		systray.AddMenuItem(message, "").Disable()
	} else if len(servers) == 0 {
		systray.AddMenuItem("연결된 Docker 서버가 없습니다", "").Disable()
	}

	// 2. 일반 웹 진입은 컨테이너 로그 선택과 독립적으로 제공한다.
	systray.AddSeparator()
	web := systray.AddMenuItem("웹으로 이동", "중앙 서버의 웹 화면 열기")
	watch(ctx, web, func() {
		openBrowser(c.base)
	})
	settings := systray.AddMenuItem("설정", "중앙 웹에서 개인 알림 설정")
	preferences := settings.AddSubMenuItem("수신 항목 설정", "받을 이벤트 항목 선택")
	watch(ctx, preferences, func() {
		openBrowser(c.base + "/#preferences")
	})
	destinations := settings.AddSubMenuItem("수신처 설정", "알림을 받을 곳 관리")
	watch(ctx, destinations, func() {
		openBrowser(c.base + "/#destinations")
	})
	quit := systray.AddMenuItem("종료", "이 PC의 트레이 앱 종료")
	watch(ctx, quit, systray.Quit)
}

// watch는 메뉴 갱신 시 이전 항목의 이벤트 대기를 취소한다.
func watch(ctx context.Context, item *systray.MenuItem, action func()) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-item.ClickedCh:
				action()
			}
		}
	}()
}

// openURL은 셸 문자열 조합 없이 OS 기본 브라우저를 실행한다.
func openURL(address string) {
	// 1. 실행 파일과 인자를 OS별로 분리해 URL을 명령으로 해석하지 않는다.
	var command *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		command = exec.Command("open", address)
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", address)
	default:
		command = exec.Command("xdg-open", address)
	}

	// 2. 메뉴 이벤트 처리를 막지 않고 실행 결과를 정리한다.
	if err := command.Start(); err != nil {
		slog.Error("브라우저를 열 수 없습니다")
		return
	}
	go func() {
		_ = command.Wait()
	}()
}

// trayIcon은 외부 자산 없이 플랫폼에서 읽을 수 있는 작은 종 아이콘을 만든다.
func trayIcon() []byte {
	// 1. 투명 배경 위에 알림 종을 그린다.
	canvas := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	teal := color.NRGBA{R: 66, G: 213, B: 183, A: 255}
	for y := 4; y < 28; y++ {
		for x := 4; x < 28; x++ {
			if (y >= 9 && y <= 22 && (x-16)*(x-16) < 64) || (y >= 22 && y <= 24 && x >= 6 && x <= 26) || ((x-16)*(x-16)+(y-26)*(y-26) <= 4) || ((x-16)*(x-16)+(y-7)*(y-7) <= 4) {
				canvas.SetNRGBA(x, y, teal)
			}
		}
	}
	var output bytes.Buffer
	_ = png.Encode(&output, canvas)
	data := output.Bytes()
	if runtime.GOOS != "windows" {
		return data
	}

	// 2. Windows에서는 PNG 그림을 ICO 컨테이너에 넣는다.
	var icon bytes.Buffer
	_ = binary.Write(&icon, binary.LittleEndian, []uint16{0, 1, 1})
	icon.Write([]byte{32, 32, 0, 0})
	_ = binary.Write(&icon, binary.LittleEndian, []uint16{1, 32})
	_ = binary.Write(&icon, binary.LittleEndian, uint32(len(data)))
	_ = binary.Write(&icon, binary.LittleEndian, uint32(22))
	icon.Write(data)
	return icon.Bytes()
}

// errPreview는 미리보기 요청에서 자격 증명이 일치하지 않음을 나타낸다.
var errPreview = errors.New("미리보기 요청 오류")
