# Whalechime

A local system tray app for monitoring events, receiving notifications, and viewing live container logs across multiple Docker servers.

**Early development release.** Monitoring and notifications run while the app is open and connected. Events missed during shutdown, sleep, or a network outage may not be fully recoverable.

- **Direct connections:** HTTP or HTTPS Docker API, optional mutual TLS (mTLS), and SSH.
- **Tray navigation:** right-click the tray icon → expand a Docker server → select a container → see its logs immediately.
- **Local settings:** manage connections, event preferences, and notification destinations without a central account.
- **Notifications:** desktop notifications plus optional email and messaging services.
- **Local storage:** encrypted settings, recent event history, and pending notification jobs on your PC.

No central server, database service, or per-server agent is required. Whalechime sends read-only Docker requests.

## Run from source

Install Go 1.25 or newer and use a desktop environment with a system tray:

```sh
go run ./desktop
```

On macOS, building from source also requires Xcode command line tools and CGO. Linux requires a StatusNotifier/AppIndicator-compatible tray and a desktop notification service.

Open **Settings → Server connections** from the tray. The current UI labels are **설정 → 서버 연결 설정**.

| Connection | Settings |
| --- | --- |
| HTTP | `http://192.168.1.10:2375`; no certificates required |
| HTTPS | `https://docker.example:2376`; optionally provide a custom CA file |
| mTLS | Enable under HTTPS and provide client certificate and private key file paths |
| SSH | `ssh://user@host:22` or `ssh://ssh-config-alias`; optionally provide an identity file |

The Docker endpoint must already be accessible. Whalechime does not reconfigure Docker, firewall rules, or host permissions. HTTP is unencrypted; restrict access to the endpoint. An unauthenticated Docker API can grant control of the host even on a LAN. HTTPS always verifies the server certificate, and SSH uses existing host-key verification. See [Docker connection security](https://docs.docker.com/engine/security/protect-access/).

Event notifications are initially disabled. Enable the events you want in **Event preferences** (`수신 항목`) to use the default **Desktop notifications** (`이 PC 알림`) destination. Add external channels under **My destinations** (`내 수신처`). Disabling notifications does not stop event collection.

## Tray and logs

Expanding a server menu does not open a window. Selecting a container opens a dedicated log view with the last 200 lines and subsequent stdout/stderr output, including timestamps. **Open web** (`웹으로 이동`) opens the local dashboard; settings use separate menu entries. Host-level Docker daemon logs are outside the default scope.

- [Tray and log viewer](docs/tray.md)
- [Connection settings](docs/connections.md)
- [Notification channels](docs/notifications.md)
- [Local storage and operations](docs/operations.md)
- [Release packages and publishing](docs/releases.md)

## Build and test

```sh
make build
make vet
make test
npm ci
npx playwright install chromium
npm run test:ui
```

The tray executable is `bin/whalechime`. Browser tests create and clean up an isolated local app and local HTTP receiver; they do not send messages to external accounts. Browser tests require Node.js 24 and npm. To run Go checks without Make, use `go vet ./...` and `go test -race ./...`.

On Windows, build and run from PowerShell at the repository root:

```powershell
go build -ldflags "-H=windowsgui" -o whalechime.exe ./desktop
.\whalechime.exe
```

Release automation builds Linux amd64, Windows amd64, macOS amd64, and macOS arm64 packages on their corresponding runners. Packages are not developer-signed or notarized. Actual tray rendering, OS notifications, and interaction need testing on each desktop. See [verification coverage](docs/verification.md).

## Operational limits

- The app retains the most recent 2,000 collected events and stores pending notification jobs separately. Docker only returns its most recent 256 historical events. See [Docker events](https://docs.docker.com/reference/cli/docker/system/events/).
- Selected events go to every enabled destination. Only failed destinations are retried, up to eight attempts. A notification can be duplicated if the app stops or saving its result fails immediately after the external service accepts it.
- SSH requires a local OpenSSH client, a remote Docker CLI, and permission to access the remote Docker socket. Interactive password prompts and automatic trust of new hosts are not supported.
- The encryption key is stored on the same PC. File encryption reduces plaintext exposure but does not protect against compromise of the entire PC or its user account.
- Shared accounts, team permissions, centralized collection, and monitoring while your PC is off are outside the current scope.

See [CONTRIBUTING.md](CONTRIBUTING.md), [SECURITY.md](SECURITY.md), and [requirements](docs/requirements.md) for development, reporting, and product scope.

## License

[Apache License 2.0](LICENSE)
