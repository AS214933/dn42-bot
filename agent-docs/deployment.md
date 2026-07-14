# Agent Deployment

This guide covers deploying the DN42 bot agent using Docker or Docker Compose.

## Docker Build

The agent ships as a multi-stage Dockerfile:

1. **Go builder** — compiles the agent binary from source.
2. **Debian runtime** — slim Debian image with all runtime dependencies.

Build the image:

```bash
cd agent-v2
docker build -t dn42-agent .
```

Or pull the prebuilt image:

```bash
docker pull ghcr.io/as214933/dn42-bot/agent-v2:latest
```

## Docker Compose

```yaml
services:
  agent:
    image: ghcr.io/as214933/dn42-bot/agent-v2:latest
    container_name: dn42-agent
    dns:
      - 172.20.0.53
      - 1.1.1.1
    network_mode: host
    cap_add:
      - NET_RAW
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
  - NET_RAW
  - NET_ADMIN
  - SYS_ADMIN
devices:
  - /dev/net/tun:/dev/net/tun
```

- **NET_RAW** — needed by the built-in NTrace-core traceroute/MTR engine for raw ICMP sockets.
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
- `vnstat` — network traffic monitoring
- `ca-certificates` — TLS certificate bundle

Traceroute/MTR is built into the agent binary via `github.com/nxtrace/NTrace-core` (GPL-3.0). TCPing is built into the agent binary using Go's `net.Dialer`; no external `traceroute`, `mtr`, or `tcping` command is required for Docker or bare-metal deployments.

Set `dns_servers` in `config.yaml` when built-in `ping`, `trace`, and `tcping` should resolve hostnames through DN42 or another explicit DNS service instead of the system resolver:

```yaml
dns_servers:
  - 172.20.0.53
  - 1.1.1.1
```

## bird-lg-go Frontend Integration

Enable `looking_glass` in the agent config to replace a separately deployed bird-lg-go proxy. The compatible routes share the existing agent port (`54321` by default), so the frontend only needs:

```dotenv
BIRDLG_PROXY_PORT=54321
```

Keep the existing frontend server/domain settings. Allow the frontend's fixed egress IPs in `looking_glass.allowed_cidrs`, and restrict port 54321 at the host firewall to the bot server and frontend wherever possible. The looking glass routes do not accept the management token; an empty allowed list denies all access. The BIRD control socket mount shown above is required, and built-in traceroute still requires `NET_RAW` or root.

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

For bare-metal deployments, run the service as root or grant the binary `CAP_NET_RAW` so built-in traceroute/MTR can open raw sockets:

```bash
sudo setcap cap_net_raw+ep /opt/dn42-agent/agent
```

## Health Checks

The agent exposes a `POST /version` endpoint that requires no authentication. Use it for health checks:

```yaml
healthcheck:
  test: ["CMD", "curl", "-sf", "-X", "POST", "http://localhost:54321/version"]
  interval: 30s
  timeout: 5s
  retries: 3
```
