# Tray navigation and container logs

## User flow

1. Run `go run ./desktop` or the built `whalechime` executable.
2. Add Docker API or SSH connections through **Settings → Server connections** (`설정 → 서버 연결 설정`).
3. Right-click the tray icon to see registered servers.
4. Expand a server to see its containers in a submenu. This step does not open a window.
5. Select a container to open its dedicated log view.
6. The last 200 lines and subsequent live output fill the view automatically without additional controls.

The dashboard, server connections, event preferences, and notification destinations have separate tray entries. No central server or user token configuration is needed. Inventory refreshes every 15 seconds, distinguishing connection failure from an empty container list.

## Log scope

Logs use Docker's `GET /containers/{id}/logs`. The app checks TTY mode, handles multiplexing and stdout/stderr, and preserves UTF-8 text. It displays server and container names with each line's timestamp and distinguishes empty logs, failures, and completed output.

The selected target is fixed to a random local view URL; the viewer does not ask you to select a server or container again. Closing the browser view or editing/removing the connection cancels the upstream request. Full container output is not stored in the local event history. The Docker logging driver must support log retrieval through the API.

## Preview and checks

```sh
go run ./desktop -demo
WHALECHIME_NATIVE_TRAY_TEST=1 dbus-run-session -- go test -race -count=1 -run TestNativeTrayHierarchy -v ./desktop
```

Preview output is synthetic and explicitly labeled as a preview. The native test uses an isolated DBus session to inspect the actual menu tree and click events. It is distinct from manually checking rendering and mouse interaction on a user's desktop.

Linux requires a StatusNotifier/AppIndicator-compatible tray. Actual Windows/macOS rendering and notifications must be checked on those platforms. macOS release packages contain a `Whalechime.app` bundle; see [release packages](releases.md).
