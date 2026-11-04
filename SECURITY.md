# Security reporting

Whalechime is in early development. Fixes target the latest `main`; there are no separately maintained long-term support versions.

Report vulnerabilities and credential exposure through [GitHub private vulnerability reporting](https://github.com/jiind/whalechime/security/advisories/new), rather than a public issue or pull request. Include the affected version, OS, connection mode, reproduction steps, and expected impact. Use dummy values and an environment you own; do not include actual keys, tokens, or private logs.

## Connections and local data

- The app sends read-only Docker requests. This does not make the server's Docker API credentials read-only.
- HTTP does not provide encryption or client authentication. Restrict which clients can reach the endpoint, including on a LAN.
- HTTPS verifies the server certificate. mTLS is optional. SSH preserves existing host-key verification.
- Settings and history are encrypted locally, but the key is stored on the same PC. Protect the user account and backups.
- Never attach local session files, encryption keys, or certificate private keys to issues, pull requests, or logs.
- Release packages are not developer-signed or notarized. SHA-256 checksums detect changes relative to the published manifest; they are not a substitute for a trusted release source.
