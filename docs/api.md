# Local app API

This is an internal contract between the tray and its embedded web interface on the PC's loopback address. It is not a central server API or a network-wide Docker proxy. Instance-specific authentication is required; unexpected hosts and cross-origin requests are rejected.

| Endpoint | Purpose |
| --- | --- |
| `GET/POST /api/servers` | List or add connections |
| `PUT/DELETE /api/servers/{id}` | Edit or remove a connection |
| `POST /api/servers/{id}/test` | Test Docker connectivity |
| `GET /api/servers/{id}/containers` | List containers |
| `GET /api/servers/{id}/containers/{container}/logs` | Recent and live output |
| `GET/PUT /api/me/preferences` | Read or replace event preferences |
| `GET/POST /api/me/destinations` | List or add notification destinations |
| `PUT/PATCH/DELETE /api/me/destinations/{id}` | Edit, enable/disable, or delete a destination |
| `POST /api/me/destinations/{id}/test` | Explicitly send a test notification |
| `GET /api/events` | Recent history filtered by server/event |
| `GET /api/me/deliveries` | Recent delivery results |
| `GET /api/event-types` | Known and newly observed event types |
| `GET /api/destination-types` | Supported destination kinds |

Mutation requests use JSON bodies. Event and delivery history responses contain at most 500 entries. `/api/events` accepts optional `server_id` and `event_key` query parameters. Log responses are newline-delimited JSON frames; `data` is Base64-encoded original output bytes. Destination read and save responses omit connection URLs, tokens, and passwords.

Upstream Docker requests are restricted to reads of `/_ping`, `/version`, `/events`, `/containers/json`, `/containers/{id}/json`, and `/containers/{id}/logs`. The SDK negotiates the API version.
