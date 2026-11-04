# Product requirements

On 2026-11-04, Whalechime changed from a central server and agent architecture to a personal local client. This document defines the current requirements; use the [verification record](verification.md) for implementation evidence and test coverage.

## Current scope

- One person connects directly from their PC to multiple Docker servers.
- No central server, PostgreSQL service, user accounts, teams, or collection agents are required.
- Each server can use HTTP/HTTPS Docker API or SSH. HTTPS can use a private CA and optional mTLS.
- mTLS requires client certificate and private key file paths. Keep HTTPS server verification and SSH host-key verification enabled.
- Do not automatically modify Docker, firewall rules, or host account permissions.
- Collect available Docker events regardless of notification settings. Send selected events to every enabled destination.
- Keep event preferences independent of destinations; do not assign separate channels per event type.
- Provide desktop notifications as the default destination, with optional email and external messaging services.
- Store settings, recent event history, and pending notification jobs on the PC. Do not expose credentials in read responses or logs.

## Tray and logs

- Right-clicking the tray icon displays a menu rather than immediately opening a web page.
- Expanding a server displays its containers in a nested tray submenu.
- Selecting a container opens a separate log view for that container.
- The log view must not require additional server selection, options, query, or play controls.
- Automatically display the last 200 lines and subsequent output across the view's content area, with server name, container name, and timestamps.
- Distinguish empty logs, connection failures, and completed output. Preserve TTY output, stdout/stderr, and UTF-8 text.
- Dashboard and settings entry points remain separate from log navigation.
- Do not provide container/image creation, deployment, start, stop, or deletion controls.

## Operational limits

- Collection and notification delivery stop when the PC or app is off or asleep.
- Docker's historical event limit prevents guaranteed recovery of every missed event.
- Read only container output available through Docker's logs API. Host-level Docker daemon logs and direct host log-file access are outside scope.
- Prevent concurrent app instances from using the same data directory. Centralized collection, account sharing, and team authorization are outside scope.
- Clearly distinguish verified behavior from untested OS interaction and actual external service delivery.

## Future work

- Installers, automatic startup, developer signing, notarization, and automatic updates
- Integration with OS credential stores
- Search/export of longer event history and per-server preferences
- Importing Docker contexts
