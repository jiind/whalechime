# Implementation and verification record

This record covers the local-client architecture introduced on 2026-11-04. Previous central-server, PostgreSQL, and agent test results are not treated as evidence for the new architecture.

## Verified behavior

| Area | Evidence |
| --- | --- |
| HTTP connections | Actual SDK HTTP inventory requests; browser save, edit, reload, and delete flows |
| HTTPS | Actual TLS communication with a private CA; rejection of untrusted server certificates |
| Optional mTLS | Successful client certificate authentication; rejection without a required client certificate |
| Invalid connection settings | Rejection of TLS settings on HTTP, passwords embedded in addresses, paths, and queries |
| SSH transport | Actual HTTP request/response through a replacement child process's stdio; fixed-command and host-verification argument checks |
| Read-only policy | An SDK container deletion request is blocked before reaching the network |
| Events and notifications | Direct event stream → local history → personal webhook; deduplication, retries, and cancellation after destination revocation |
| Actual Docker | A test-only loopback HTTP proxy to the existing Docker socket collects a real container creation event and UTF-8 logs |
| Log lifetime | Last-200-lines, follow, and timestamp options; deletion of a connection cancels its upstream stream |
| Local persistence | Credentials absent from plaintext storage and read responses; restart recovery, duplicate-instance lock, and rejected mutation rollback |
| Local web boundary | Instance-specific HttpOnly session; rejection of missing authentication, external Host/Origin, and form requests |
| Browser flows | Independent preferences/destinations, HTTP/HTTPS/mTLS/SSH form selection, and a 390px mobile viewport |
| Tray menu | Actual DBus menu tree in an isolated session; expanding a server does not open a window, while container, dashboard, and settings entries have separate behavior |

## Commands used

```sh
gofmt -l cmd internal desktop
go vet ./...
go test -race -count=1 ./...
make build
WHALECHIME_NATIVE_TRAY_TEST=1 dbus-run-session -- go test -race -count=1 -run TestNativeTrayHierarchy -v ./desktop
GOOS=windows CGO_ENABLED=0 go build -o /tmp/whalechime.exe ./desktop
WHALECHIME_TEST_DOCKER=1 go test -race -count=1 -run TestRealDockerDirect -v ./internal/localapp
npm run test:ui
git diff --check
```

Local browser checks used an installed Chrome via `WHALECHIME_TEST_BROWSER_PATH`. GitHub's `browser` job installs Playwright Chromium and runs the same scenarios.

Both `go` and `browser` passed for implementation commit `d489909` in [this Actions run](https://github.com/jiind/whalechime/actions/runs/41792336765). The [merged main commit](https://github.com/jiind/whalechime/actions/runs/41794575159) also passed. Check each later commit's own run rather than assuming earlier results apply unchanged.

The real Docker test requires a local `alpine:3.22` image. It creates and removes only its own container and does not reconfigure the daemon or host permissions. This is distinct from testing an externally exposed API endpoint on another machine.

## Release checks

The [release workflow](releases.md) includes native Go checks and archive creation for Linux amd64, Windows amd64, macOS amd64, and macOS arm64. Pull requests involving release-related code run package validation without publishing. Package structure and SemVer validation use Python standard-library tests:

```sh
python3 -m unittest discover -s tests/release -p 'test_*.py' -v
```

Review the corresponding Actions run for successful native checks. No release has been published solely to test the workflow. Archive generation and unit tests do not verify interactive desktop behavior, developer signing, or notarization.

## Environments still requiring verification

- Real remote SSH authentication, host keys, and Docker CLI combinations. The automated transport test substitutes the SSH process boundary.
- Actual desktop tray rendering, clicks, and OS notifications on Windows and macOS.
- Real SMTP and messaging-service credentials and final delivery. Automated checks use local receivers and request contracts.
- User desktop notification restrictions, Do Not Disturb settings, and network recovery after sleep.
