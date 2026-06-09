# Agent Deployment

This guide covers deploying the DN42 bot agent using Docker or Docker Compose.

## Docker Build

The agent ships as a multi-stage Dockerfile:

1. **Go builder** — compiles the agent binary from source.
2. **tcping builder** — builds [tcping](https://github.com/pouriyajamshidi/tcping) from source.
3. **Debian runtime** — slim Debian image with all runtime dependencies.

Build the image:

```bash
cd agent-v2
docker build -t dn42-agent .
```

Or pull the prebuilt image:

```bash
docker pull ghcr.io/bingxin666/dn42-bot/agent:latest
```

## Docker Compose

```yaml
services:
  agent:
    image: ghcr.io/bingxin666/dn42-bot/agent:latest
    container_name: dn42-agent
    dns:
      - 172.20.0.53
      - 1.1.1.1
    network_mode: host
    cap_add:
      - NET_ADMIN
      - SYS_ADMIN
    devices:
      - /dev/net/tun:/dev/net/tun
    restart: unless-stopped
    volumes:
      - /etc/wireguard:/etc/wireguard
      - /etc/bird/dn42_peers:/etc/bird/dn42_peers
      - /etc/bird/config:/etc/bird/config
      - /var/run/bird/bird.ctl:/var/run/bird/bird.ctl
      - ./config.yaml:/app/config.yaml:ro
    environment:
      - CONFIG_PATH=/app/config.yaml
```

## Volume Mounts

The agent needs access to WireGuard and BIRD configuration directories on the host.

| Container Path | Host Path (example) | Purpose |
|----------------|---------------------|---------|
| `/etc/wireguard` | `/etc/wireguard` | WireGuard peer configs. The agent creates and removes `dn42-*.conf` files here. |
| `/etc/bird/dn42_peers` | `/etc/bird/dn42_peers` | BIRD peering configs. The agent writes per-peer BIRD config snippets here. |
| `/etc/bird/config` | `/etc/bird/config` | Main BIRD configuration directory. |
| `/var/run/bird/bird.ctl` | `/var/run/bird/bird.ctl` | BIRD control socket. Used for route lookups and protocol status queries. |

Mount the config file as read-only:

```yaml
volumes:
  - ./config.yaml:/app/config.yaml:ro
```

## Network Mode

The agent **must** run in `host` network mode. WireGuard tunnels need direct access to the host's network stack to create interfaces and route traffic.

```yaml
network_mode: host
```

Without host networking, WireGuard interfaces won't function and peering will fail.

## Capabilities

The agent requires elevated privileges for network operations:

```yaml
cap_add:
  - NET_ADMIN
  - SYS_ADMIN
devices:
  - /dev/net/tun:/dev/net/tun
```

- **NET_ADMIN** — needed to create/configure WireGuard interfaces and manage network routes.
- **SYS_ADMIN** — needed for some WireGuard operations.
- **/dev/net/tun** — TUN device for WireGuard tunnel creation.

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `CONFIG_PATH` | `config.yaml` | Path to the YAML config file inside the container. |

## Runtime Dependencies

The Docker image includes these packages (installed automatically):

- `bird2` — BIRD routing daemon
- `wireguard-tools` — WireGuard utilities (`wg`, `wg-quick`)
- `iproute2` — network configuration tools (`ip`)
- `iputils-ping` — ICMP ping
- `traceroute` — path tracing
- `vnstat` — network traffic monitoring
- `ca-certificates` — TLS certificate bundle
- `tcping` — TCP connectivity testing (built from source)

## Systemd Service (Non-Docker)

For non-Docker deployments, run the agent as a systemd service:

```ini
[Unit]
Description=DN42 Bot Agent
After=network.target bird2.service wg-quick.target

[Service]
Type=simple
ExecStart=/opt/dn42-agent/agent
WorkingDirectory=/opt/dn42-agent
Restart=on-failure
RestartSec=5
Environment=CONFIG_PATH=/opt/dn42-agent/config.yaml

[Install]
WantedBy=multi-user.target
```

Place the compiled binary and config in `/opt/dn42-agent/`.

## Health Checks

The agent exposes a `POST /version` endpoint that requires no authentication. Use it for health checks:

```yaml
healthcheck:
  test: ["CMD", "curl", "-sf", "-X", "POST", "http://localhost:54321/version"]
  interval: 30s
  timeout: 5s
  retries: 3
```
