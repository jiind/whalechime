//go:build linux

package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"fyne.io/systray"
	"github.com/godbus/dbus/v5"
)

// nativeLayout은 OS 트레이가 실제로 읽는 DBus 메뉴 트리다.
type nativeLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// findNative는 OS 메뉴의 실제 하위 항목에서 표시 이름을 찾는다.
func findNative(layout nativeLayout, label string) (nativeLayout, bool) {
	if value, ok := layout.Props["label"]; ok && value.Value() == label {
		return layout, true
	}
	for _, variant := range layout.Children {
		var child nativeLayout
		if err := dbus.Store([]any{variant.Value()}, &child); err != nil {
			continue
		}
		if found, ok := findNative(child, label); ok {
			return found, true
		}
	}
	return nativeLayout{}, false
}

// TestNativeTrayHierarchy는 OS 메뉴의 펼침과 클릭을 통해 웹·로그 이동을 구분한다.
func TestNativeTrayHierarchy(t *testing.T) {
	// 1. 사용자 데스크톱과 분리한 명시적 DBus 검증에서만 실행한다.
	if os.Getenv("WHALECHIME_NATIVE_TRAY_TEST") != "1" {
		t.Skip("dbus-run-session에서 명시적으로 실행")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base, stop := startDemo()
	defer stop()
	c, _ := newConnection(base, "preview-only")
	servers, err := c.inventory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v, local, listener, err := newViewer(c)
	if err != nil {
		t.Fatal(err)
	}
	go local.Serve(listener)
	defer stopServer(local)
	opened := make(chan string, 4)
	previous := openBrowser
	openBrowser = func(address string) {
		opened <- address
	}
	defer func() {
		openBrowser = previous
	}()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		systray.Run(func() {
			systray.SetIcon(trayIcon())
			render(ctx, c, v, servers, "")
		}, func() {})
	}()
	defer func() {
		cancel()
		systray.Quit()
		<-ended
	}()
	bus, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	object := bus.Object(fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid()), "/StatusNotifierItem/menu")
	deadline := time.Now().Add(5 * time.Second)
	var layout nativeLayout
	var revision uint32
	var parent, leaf, web nativeLayout

	// 2. OS의 실제 메뉴 트리에서 서버 옆 하위 컨테이너를 확인한다.
	for time.Now().Before(deadline) {
		err = object.Call("com.canonical.dbusmenu.GetLayout", 0, int32(0), int32(-1), []string{}).Store(&revision, &layout)
		if err == nil {
			var exists bool
			parent, exists = findNative(layout, "운영 서버 · 미리보기")
			if exists {
				leaf, exists = findNative(parent, "payment-api · running")
				if exists {
					web, _ = findNative(layout, "웹으로 이동")
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if leaf.ID == 0 || web.ID == 0 || len(parent.Children) != 2 {
		t.Fatalf("네이티브 서버·컨테이너 메뉴 구성 실패: %v", err)
	}
	err = object.Call("com.canonical.dbusmenu.Event", 0, parent.ID, "opened", dbus.MakeVariant(int32(0)), uint32(0)).Err
	if err != nil {
		t.Fatal(err)
	}
	select {
	case address := <-opened:
		t.Fatalf("서버 펼침에서 창이 열림: %s", address)
	default:
	}

	// 3. 컨테이너 클릭만 로그 화면을 열고 웹 이동은 로컬 주소만 연다.
	if err := object.Call("com.canonical.dbusmenu.Event", 0, leaf.ID, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case address := <-opened:
		if !strings.HasPrefix(address, v.base+"/view/") {
			t.Fatal("컨테이너 선택이 로그 전용 화면으로 연결되지 않았습니다")
		}
		response, err := http.Get(address)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal("선택한 컨테이너의 로그 화면을 열 수 없습니다")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("컨테이너 클릭이 처리되지 않았습니다")
	}
	if err := object.Call("com.canonical.dbusmenu.Event", 0, web.ID, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case address := <-opened:
		if address != c.pageURL() {
			t.Fatal("웹 이동과 로그 화면이 분리되지 않았습니다")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("웹 이동 클릭이 처리되지 않았습니다")
	}
	preferences, found := findNative(layout, "수신 항목 설정")
	if !found {
		t.Fatal("로컬 웹으로 연결하는 설정 메뉴가 없습니다")
	}
	if err := object.Call("com.canonical.dbusmenu.Event", 0, preferences.ID, "clicked", dbus.MakeVariant(int32(0)), uint32(0)).Err; err != nil {
		t.Fatal(err)
	}
	select {
	case address := <-opened:
		if address != c.pageURL()+"#preferences" {
			t.Fatal("설정 메뉴가 로컬 웹의 개인 설정에 연결되지 않습니다")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("설정 메뉴 클릭이 처리되지 않았습니다")
	}
	t.Log("네이티브 메뉴 검증: 서버 펼침은 화면 이동 없음, 컨테이너 클릭은 로그 전용 화면, 웹 이동은 별도 주소")
}
