# Whalechime contributor instructions

- Write public documentation and templates in English. Keep explanatory source comments in Korean. Add a concise Korean role comment above every explicit function or method; use numbered Korean comments for multiple processing stages.
- Prefer the Go standard library. Validate Go changes with `gofmt`, `go vet`, and `go test -race ./...`.
- This is a local tray app for one person connecting directly to multiple Docker servers. Do not make a central server, PostgreSQL, accounts, teams, or collection agents required infrastructure.
- Configure HTTP/HTTPS API or SSH per server. TLS and mTLS are optional; mTLS applies only to HTTPS. Preserve HTTPS server certificate verification and SSH host-key verification.
- Collect Docker events regardless of notification preferences. Keep event selections independent of notification destinations; do not map individual event types to channels.
- Store settings, history, and pending deliveries on the user's PC. Never expose raw credentials or tokens in logs or read responses.
- Keep server and container selection inside tray submenus. Only selecting a container opens its dedicated log view. Do not add Docker control APIs.
- Use `docs/requirements.md` to distinguish current scope from future work.
- Release workflows may validate and build packages in pull requests, but publication requires an explicit version tag. Do not create a tag or publish a release unless the user requests it.
