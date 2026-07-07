# Migrating from Agent v1 (Python) to Agent v2 (Go)

This guide covers migrating from the original Python-based agent to the new Go-based agent v2.

## What Changed

### 1. Config Format: JSON → YAML

The agent v1 used `agent_config.json` (JSON). Agent v2 uses `config.yaml` (YAML).

**v1 (`agent_config.json`):**
```json
{
  "HOST": "0.0.0.0",
  "PORT": 54321,
  "SECRET": "secret_token",
  "OPEN": true,
  "MAX_PEERS": 0,
  "NET_SUPPORT": {
    "ipv4": true,
    "ipv6": true,
    "ipv4_nat": false,
    "cn": false
  },
  "MY_DN42_LINK_LOCAL_ADDRESS": "fe80::1816",
  "MY_DN42_ULA_ADDRESS": "fd2c:1323:4042::1",
  "MY_DN42_IPv4_ADDRESS": "172.23.246.1",
  "MY_WG_PUBLIC_KEY": "LUwqKS6QrCPv510Pwt1eAIiHACYDsbMjrkrbGTJfviU=",
  "BIRD_CTL_PATH": "/var/run/bird/bird.ctl",
  "BIRD_TABLE_4": "master4",
  "BIRD_TABLE_6": "master6",
  "VNSTAT_AUTO_ADD": true,
  "VNSTAT_AUTO_REMOVE": false,
  "DEFAULT_MTU": 1420,
  "SERVER_URL": "example.dn42"
}
```

**v2 (`config.yaml`):**
```yaml
host: "0.0.0.0"
port: 54321
secret: "secret_token"
open: true
max_peers: 0
net_support:
  ipv4: true
  ipv6: true
  ipv4_nat: false
  cn: false
my_dn42_link_local_address: "fe80::1816"
my_dn42_ula_address: "fd2c:1323:4042::1"
my_dn42_ipv4_address: "172.23.246.1"
my_wg_public_key: "LUwqKS6QrCPv510Pwt1eAIiHACYDsbMjrkrbGTJfviU="
bird_ctl_path: "/var/run/bird/bird.ctl"
bird_table_4: "master4"
bird_table_6: "master6"
vnstat_auto_add: true
vnstat_auto_remove: false
default_mtu: 1420
server_url: "example.dn42"
```

### 2. Config Keys: Uppercase → snake_case

All config keys changed from `UPPER_CASE` to `snake_case`:

| v1 Key | v2 Key |
|--------|--------|
| `HOST` | `host` |
| `PORT` | `port` |
| `SECRET` | `secret` |
| `OPEN` | `open` |
| `MAX_PEERS` | `max_peers` |
| `MIN_PEER_REQUIREMENT` | `min_peer_requirement` |
| `NET_SUPPORT` | `net_support` |
| `EXTRA_MSG` | `extra_msg` |
| `MY_DN42_LINK_LOCAL_ADDRESS` | `my_dn42_link_local_address` |
| `MY_DN42_ULA_ADDRESS` | `my_dn42_ula_address` |
| `MY_DN42_IPv4_ADDRESS` | `my_dn42_ipv4_address` |
| `MY_WG_PUBLIC_KEY` | `my_wg_public_key` |
| `SENTRY_DSN` | `sentry_dsn` |
| `BIRD_CTL_PATH` | `bird_ctl_path` |
| `BIRD_TABLE_4` | `bird_table_4` |
| `BIRD_TABLE_6` | `bird_table_6` |
| `VNSTAT_AUTO_ADD` | `vnstat_auto_add` |
| `VNSTAT_AUTO_REMOVE` | `vnstat_auto_remove` |
| `DEFAULT_MTU` | `default_mtu` |
| `SERVER_URL` | `server_url` |

### 3. Agent Version: 29 → 30

The agent version bumped from 29 to 30. The server checks this version number. Make sure your server supports agent version 30 before upgrading.

### 4. Binary: Python Script → Go Binary

- **v1:** Python script requiring `aiohttp`, `sentry-sdk`, `traceroute`, and `tcping` as separate dependencies.
- **v2:** Statically compiled Go binary. No runtime dependencies needed beyond the system packages (WireGuard, BIRD, etc.).

### 5. Dependencies

| v1 (Python) | v2 (Go) |
|-------------|---------|
| `aiohttp` | `go-chi/chi` (HTTP router, built into binary) |
| `sentry-sdk` | `sentry-go` (error tracking, built into binary) |
| `traceroute` / `mtr` system binaries | `github.com/nxtrace/NTrace-core` (built into the agent binary, GPL-3.0) |
| `tcping` system binary | Go `net.Dialer` TCP connect probes (built into the agent binary) |

The Go binary bundles HTTP, error tracking, traceroute/MTR, and TCPing support. No `pip install`, `traceroute`, `mtr`, or `tcping` installation is required. Built-in traceroute/MTR still needs root or `CAP_NET_RAW` on bare metal.

### 6. Config File Path

- **v1:** `agent/agent_config.json` (default)
- **v2:** `config.yaml` (default), configurable via `CONFIG_PATH` env var

### 7. Config File Mount

In Docker Compose, update the volume mount:

```yaml
# v1
volumes:
  - ./agent_config.json:/app/agent_config.json:ro

# v2
volumes:
  - ./config.yaml:/app/config.yaml:ro
```

## Migration Steps

1. **Back up** your current `agent_config.json`.

2. **Convert the config** from JSON to YAML. Use the key mapping table above. Remove any keys that don't exist in v2, and add the new v2-specific keys.

3. **Test the config** by running the agent locally:
   ```bash
   CONFIG_PATH=config.yaml ./agent
   ```

4. **Update Docker Compose** (if applicable):
   - Change the image tag to `agent-v2:latest`.
   - Update the volume mount from `agent_config.json` to `config.yaml`.
   - The `CONFIG_PATH` environment variable defaults to `config.yaml`, so it usually doesn't need to be set explicitly.

5. **Verify the server** supports agent version 30. The server's `API_TOKEN` must match the agent's `secret`.

6. **Restart the agent** and check logs for any config parsing errors.

7. **Verify peering** works by creating a test peer through the Telegram bot.

## Backward Compatibility

Agent v2 implements the same HTTP API contract that the server uses with agent v1:

- `POST /version`
- `POST /config/get`
- `POST /peer`
- `POST /remove`
- `POST /info`
- `POST /pre_peer`
- `POST /restart`
- `POST /errorlist`
- `POST /ping`
- `POST /trace`
- `POST /tcping`
- `POST /route`
- `POST /path`
- `POST /igp_topology`

Agent v2 also adds `POST /listpeers` for the server's v2-only peer import/export workflow.
Agent v2 also exposes `POST /update/check` and `POST /update/apply` for bare-metal agent release updates.

No server-side changes are needed to support agent v2 beyond ensuring the `API_TOKEN` matches.

## Troubleshooting

**Config parsing errors:** Make sure all IP addresses are quoted strings in YAML. YAML parses unquoted values like `fe80::1816` as strings anyway, but quoting is safer.

**WireGuard interface errors:** Verify that `NET_ADMIN`, `SYS_ADMIN`, and `/dev/net/tun` are available to the container.

**BIRD control socket errors:** Check that `bird_ctl_path` points to the correct socket and the socket file is mounted into the container.
