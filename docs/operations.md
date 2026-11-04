# Local storage and operations

## Running and choosing a data directory

```sh
go run ./desktop
# Use a separate directory for another local configuration or test environment.
go run ./desktop -data-dir /path/to/private/directory
```

The default directory is `whalechime` under Go's `os.UserConfigDir()`: usually `~/.config/whalechime` on Linux, `~/Library/Application Support/whalechime` on macOS, and a directory under the user's `%AppData%` on Windows.

- `state.enc`: AES-256-GCM-encrypted connections, preferences, recent events, and pending deliveries
- `key`: the local encryption key needed to restore that state
- `.lock`: an OS file lock preventing concurrent access to the same data directory

Files are created for the current user. **Back up both the encrypted state and its key**, and restrict access to backups. The state cannot be restored without the original key; losing the key does not cause the app to overwrite or reset the existing encrypted state. File encryption does not protect against compromise of the entire user account or PC.

The app retains the most recent 2,000 collected events. Pending delivery jobs are stored independently of that history limit; failed destinations get up to eight attempts. If the app stops or result persistence fails immediately after a service accepts a notification, it may be sent again. Collection and delivery stop while the app or PC is off or asleep.

## Local web interface

Open the UI through the tray's dashboard or settings entries. The app binds only to a random loopback port and does not create a publicly accessible server. An instance-specific HttpOnly session cookie is issued through the tray's launch URL. Requests with an unexpected Host, external Origin, or no authentication are rejected. Users do not need to enter a separate access token.

Container logs use a separate local view. Docker credentials are not put in browser URLs or HTML. Closing the view or removing the server connection cancels the remote log request.

## Notification connections

The default desktop destination works in the tray app. Linux uses the desktop DBus notification service, macOS uses Notification Center, and Windows uses PowerShell and system notifications. OS notification restrictions or Do Not Disturb settings may suppress display. See [notification channels](notifications.md) for external services.

If an internal notification receiver needs HTTP or a private address, set `WHALECHIME_ALLOWED_NOTIFICATION_HOSTS=host1,host2` when launching the app. This setting applies to notification destinations, not Docker connection addresses.

## Headless development and diagnostics

```sh
go run ./cmd/whalechime -data-dir /path/to/test-data -session-file /path/to/private/session.json
```

This entry point runs the local web app without a tray for testing or diagnostics. Open the session file's `web_url` and keep the file private. The launch URL is not logged, an existing session file is not overwritten, and the file is removed on normal shutdown. Check desktop notifications through the tray app. Browser tests create this isolated environment automatically.
