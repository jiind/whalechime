# Notification destinations

Use **Event preferences** (`수신 항목`) to choose events and **My destinations** (`내 수신처`) to configure receivers. An enabled event goes to every active destination; channels are not assigned separately per event type.

After saving a destination, use **Send test** (`테스트 발송`) to check it. This explicit action is available even when the destination is disabled. Existing credentials are never returned for editing; enter the required connection values again.

| Kind | Configuration | Notes |
| --- | --- | --- |
| Desktop | Tray app and OS notification service | Added by default. Event preferences still need to be enabled. |
| Email | SMTP host/port, optional username/password, sender and recipient | Port 465 uses TLS; other ports use STARTTLS. TLS SMTP without authentication is also supported. |
| Slack | Incoming Webhook URL | Install the app in the destination conversation and use its generated URL. |
| Teams | Workflows Webhook URL | Use the `When a Teams webhook request is received` workflow trigger. |
| Discord | Channel Webhook URL | Automatic mentions are disabled. |
| Telegram | Bot token and chat ID | Start a conversation with the bot or add it to the destination group. |
| Google Chat | Space Webhook URL | The organization must permit webhooks. |
| Mattermost | Incoming Webhook URL | Self-hosted endpoints are supported. |
| ntfy | Topic URL and optional Bearer token | Example: `https://ntfy.example.com/docker-notifications` |
| Gotify | Server base URL and application token | Uses the server's `/message` API. |
| Pushover | Application token and user key | Uses the service's message API. |
| Custom Webhook | HTTP(S) URL and optional Bearer token | Sends a JSON object with `title` and `message`. |

`GET /api/destination-types` returns desktop notifications and the eleven implemented external channel types. Additional providers, such as SMS or browser push, require their own sender implementation and are not shown as available options.

## Internal receiver addresses

External destinations use public HTTPS endpoints by default. For private Mattermost, Gotify, SMTP, or webhook hosts, set `WHALECHIME_ALLOWED_NOTIFICATION_HOSTS` to a comma-separated list of hostnames or IPs when launching the app. Each host exception also permits HTTP, so include only receivers you trust.

The sender checks the resolved IP used for the actual connection and does not follow redirects. HTTP response bodies or URLs that could contain credentials are not stored in delivery errors.

## Delivery results

A successful external delivery means the service accepted the request. It does not prove that a person read it or an email reached its final mailbox. Only failed destinations are retried, with exponential delays and at most eight attempts. Current event preferences, destination status, and server monitoring state are checked before retrying.

Test real accounts explicitly after configuration. Automated checks use local receivers and provider request contracts; they do not send messages to real users or organizations.
