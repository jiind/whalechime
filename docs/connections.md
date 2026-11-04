# Docker connections

Use **Settings → Server connections** in the tray to configure a name, address, and monitoring state. The current UI labels are `설정 → 서버 연결 설정`. You can register up to 100 servers. Failure of one connection does not stop collection from the others.

## HTTP

Use `http://host:port`. No TLS, CA, or client certificate configuration is required. Traffic is unencrypted, and a LAN address alone does not restrict access to the Docker API. Manage allowed networks and clients on the server. The app does not enable remote Docker access or open firewall ports automatically.

## HTTPS and optional mTLS

Entering `https://host:port` displays the HTTPS settings.

- Leave the CA file path empty to use system-trusted CAs. To trust a private CA, provide a local PEM file path.
- Server identity and certificate validity are always verified.
- If the endpoint requires a client certificate, enable mTLS and provide local PEM paths for the client certificate and private key.
- Turning off mTLS keeps HTTPS server verification enabled. Client authentication and server verification are separate concerns.
- Files must be readable by the user running the app. Do not paste private key contents into the UI, read responses, or logs.

TLS is optional because HTTP connections are also supported. mTLS is an additional option available only with HTTPS.

## SSH

Use `ssh://user@host:port` or `ssh://ssh-config-alias`. The local OpenSSH client's configuration, keys, and authentication agent are reused. You can optionally specify an identity file.

- Connections use `BatchMode=yes` and `StrictHostKeyChecking=yes`; there are no interactive password or host-trust prompts.
- Verify new host keys and add them to `known_hosts` yourself. The app does not modify the trust list.
- The remote command is fixed to `docker system dial-stdio` to transport Docker API requests. Arbitrary shell commands are not accepted.
- The remote account needs Docker socket access and an available Docker CLI.

## Editing and removing connections

You can save a connection while its server is offline. Use **Test connection** (`연결 확인`) to verify an actual Docker response. Editing, disabling, or removing a connection cancels its existing event and log streams; it does not change the server's containers.

The transport permits only the requests needed for version negotiation, container lists, events, and logs. This does not replace server-side authorization. See [Docker connection security](https://docs.docker.com/engine/security/protect-access/).
