# Agent API Reference

Management endpoints use POST. The optional bird-lg-compatible looking glass endpoints use GET. The agent listens on the address configured in `config.yaml` (default: `0.0.0.0:54321`).

## Authentication

All management endpoints except `/version` require authentication via the `X-DN42-Bot-Api-Secret-Token` header. The optional `/bird`, `/bird6`, `/traceroute`, and `/traceroute6` routes use the `looking_glass` source policy instead because bird-lg-go frontend does not send this header.

```
X-DN42-Bot-Api-Secret-Token: <your-secret-token>
```

Requests without a valid token receive `403 Forbidden`.

---

## Core

### POST /version

Returns the agent version number. No authentication required.

**Request:** Empty body.

**Response:**
- `Content-Type: text/plain`
- Body: version number followed by newline (e.g., `30\n`)

**Status Codes:**
- `200 OK` — Always

**Example:**
```bash
curl -X POST http://agent-host:54321/version
```

Response: `30`

---

## Config

### POST /config/get

Returns selected agent configuration fields.

**Request Body (JSON, optional):**
```json
{
  "keys": ["DEFAULT_MTU", "OPEN", "NET_SUPPORT"]
}
```

If `keys` is omitted or empty, all displayable fields are returned.

Available keys: `DEFAULT_MTU`, `OPEN`, `MAX_PEERS`, `MIN_PEER_REQUIREMENT`, `EXTRA_MSG`, `MY_DN42_IPv4_ADDRESS`, `VNSTAT_AUTO_ADD`, `VNSTAT_AUTO_REMOVE`, `BIRD_CTL_PATH`, `BIRD_TABLE_4`, `BIRD_TABLE_6`, `NET_SUPPORT`, `LOOKING_GLASS`

**Response (JSON):**
```json
{
  "DEFAULT_MTU": 1420,
  "OPEN": true,
  "MAX_PEERS": 10,
  "MIN_PEER_REQUIREMENT": 1,
  "NET_SUPPORT": {
    "ipv4": true,
    "ipv6": true,
    "ipv4_nat": false,
    "cn": false
  }
}
```

**Status Codes:**
- `200 OK` — Success
- `403 Forbidden` — Invalid or missing token

**Example:**
```bash
curl -X POST http://agent-host:54321/config/get \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"keys": ["DEFAULT_MTU", "OPEN"]}'
```

To get all fields:
```bash
curl -X POST http://agent-host:54321/config/get \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

`LOOKING_GLASS` returns the normalized enable flags, source lists, concurrency limits, timeout, query limit, and output limit.

---

## Peer Management

### POST /pre_peer

Returns pre-peer information: current peer count, limits, network support, and extra message. Used by the server to check if peering is possible before creating a peer.

**Request Body:** Empty.

**Response (JSON):**
```json
{
  "existed": 5,
  "max": 10,
  "requirement": 1,
  "open": true,
  "net_support": {
    "ipv4": true,
    "ipv6": true,
    "ipv4_nat": false,
    "cn": false
  },
  "lla": "fe80::xxxx:xxxx:xxxx:xxxx",
  "msg": ""
}
```

| Field | Type | Description |
|-------|------|-------------|
| `existed` | int | Current number of WireGuard peers |
| `max` | int | Maximum allowed peers (0 = unlimited) |
| `requirement` | int | Minimum peers required to peer with this node |
| `open` | bool | Whether peering is currently open |
| `net_support` | object | Supported network protocols |
| `lla` | string | Agent's DN42 link-local address |
| `msg` | string | Extra message from config |

**Status Codes:**
- `200 OK` — Success
- `403 Forbidden` — Invalid or missing token
- `500 Internal Server Error` — Failed to count peers, or WireGuard/BIRD peer config counts do not match

**Example:**
```bash
curl -X POST http://agent-host:54321/pre_peer \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

---

### POST /info

Returns detailed WireGuard and BIRD session information for a specific peer.

**Request Body (plain text):**
```
4242421234
```

The body contains a single integer: the peer's ASN.

**Response (JSON):**
```json
{
  "port": "20000",
  "mtu": 1420,
  "v6": "fd42:xxxx::2",
  "v4": "172.20.x.x",
  "clearnet": "",
  "pubkey": "xxxxxxxxxx=",
  "psk": "",
  "desc": "My Peer",
  "session": "IPv6 Session with IPv6 & IPv4 Channels",
  "session_name": ["DN42_4242421234_v6", "DN42_4242421234_v4"],
  "my_v6": "fd42:xxxx::1",
  "my_v4": "172.20.x.x",
  "my_pubkey": "yyyyyyyyyy=",
  "wg_last_handshake": 1700000000,
  "wg_transfer": [1024000, 2048000],
  "bird_status": {
    "DN42_4242421234_v6": ["Established", "BGP", {"v6": "100"}],
    "DN42_4242421234_v4": ["Established", "BGP", {"v4": "50"}]
  },
  "net_support": {
    "ipv4": true,
    "ipv6": true,
    "ipv4_nat": false,
    "cn": false
  },
  "lla": "fe80::xxxx:xxxx:xxxx:xxxx"
}
```

| Field | Type | Description |
|-------|------|-------------|
| `port` | string | WireGuard listening port |
| `mtu` | int | MTU value |
| `v6` | string | Peer's IPv6 address (ULA or link-local) |
| `v4` | string | Peer's IPv4 address |
| `clearnet` | string | Peer's clearnet address |
| `pubkey` | string | Peer's WireGuard public key |
| `psk` | string | Preshared key (empty if none) |
| `desc` | string | BGP session description |
| `session` | string | Session type description |
| `session_name` | array | BIRD session names (e.g., `DN42_4242421234_v6`) |
| `my_v6` | string | Agent's IPv6 address for this peer |
| `my_v4` | string | Agent's IPv4 address for this peer |
| `my_pubkey` | string | Agent's WireGuard public key |
| `wg_last_handshake` | int | Unix timestamp of last WireGuard handshake |
| `wg_transfer` | array | `[rx_bytes, tx_bytes]` transfer counters |
| `bird_status` | object | Map of session name to `[state, info, routes]` |
| `net_support` | object | Agent's network support flags |
| `lla` | string | Agent's link-local address |

**Status Codes:**
- `200 OK` — Success
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Invalid ASN in body
- `404 Not Found` — No WireGuard or BIRD config exists for this ASN
- `500 Internal Server Error` — Config read/parse failure, or mismatched WG/BIRD configs

**Example:**
```bash
curl -X POST http://agent-host:54321/info \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "4242421234"
```

---

### POST /peer

Creates or updates a WireGuard and BIRD peer configuration, brings up the WireGuard interface, and reloads BIRD.

**Request Body (JSON):**
```json
{
  "ASN": 4242421234,
  "Contact": "admin@example.com",
  "Port": 20000,
  "IPv4": "172.20.x.x",
  "IPv6": "fd42:xxxx::2",
  "PublicKey": "xxxxxxxxxx=",
  "PresharedKey": "",
  "Clearnet": "example.com",
  "Channel": "IPv6 & IPv4",
  "MP-BGP": "IPv6",
  "MTU": 1420,
  "Request-LinkLocal": ""
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ASN` | int | Yes | Peer's ASN |
| `Contact` | string | Yes | Peer contact info |
| `Port` | int | Yes | WireGuard port |
| `IPv4` | string | No | Peer's IPv4 address |
| `IPv6` | string | No | Peer's IPv6 address |
| `PublicKey` | string | Yes | Peer's WireGuard public key |
| `PresharedKey` | string | No | Preshared key (empty if none) |
| `Clearnet` | string | No | Clearnet reachability info |
| `Channel` | string | Yes | One of: `"IPv6 only"`, `"IPv4 only"`, `"IPv6 & IPv4"` |
| `MP-BGP` | string | Yes | One of: `"IPv6"`, `"IPv4"`, `"Not supported"` |
| `MTU` | int | No | MTU value (defaults to agent default, then 1420) |
| `Request-LinkLocal` | string | No | Link-local address request |

**Status Codes:**
- `200 OK` — Peer created/updated successfully
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Malformed JSON body
- `503 Service Unavailable` — Peer limit reached or peering is closed

**Example:**
```bash
curl -X POST http://agent-host:54321/peer \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{
    "ASN": 4242421234,
    "Contact": "admin@example.com",
    "Port": 20000,
    "IPv6": "fd42:xxxx::2",
    "PublicKey": "xxxxxxxxxx=",
    "Channel": "IPv6 & IPv4",
    "MP-BGP": "IPv6",
    "MTU": 1420
  }'
```

---

### POST /remove

Removes a peer's WireGuard and BIRD configurations, tears down the interface, and reloads BIRD.

**Request Body (plain text):**
```
4242421234
```

The body contains a single integer: the ASN to remove.

**Status Codes:**
- `200 OK` — Peer removed successfully
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Invalid or empty ASN

**Example:**
```bash
curl -X POST http://agent-host:54321/remove \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "4242421234"
```

---

### POST /restart

Restarts a peer's WireGuard interface and BIRD sessions.

**Request Body (plain text):**
```
4242421234
```

The body contains a single integer: the ASN to restart.

**Status Codes:**
- `200 OK` — Restart succeeded
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Invalid or empty ASN
- `404 Not Found` — WireGuard interface does not exist (both v4 and v6 BIRD sessions have syntax errors and WG failed)
- `500 Internal Server Error` — BIRD restart syntax error, or WireGuard bring-up failure

**Example:**
```bash
curl -X POST http://agent-host:54321/restart \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "4242421234"
```

---

## Agent Update

### POST /update/check

Checks GitHub releases for an agent-v2 binary matching the current OS and architecture.

**Request Body (JSON, optional):**
```json
{
  "channel": "candidate"
}
```

`channel` may be `candidate` or `stable`. If omitted, the configured `auto_update.channel` is used.

**Response (JSON):**
```json
{
  "current_version": "v2.0.0-alpha.3",
  "build_commit": "abcdef0",
  "channel": "candidate",
  "latest_version": "v2.0.0-alpha.4",
  "update_available": true,
  "installed": false,
  "restart_required": false,
  "prerelease": true,
  "release_url": "https://github.com/AS214933/dn42-bot/releases/tag/v2.0.0-alpha.4",
  "asset_name": "agent-v2-linux-amd64"
}
```

**Status Codes:**
- `200 OK` — Check succeeded
- `403 Forbidden` — Invalid or missing token
- `502 Bad Gateway` — Release lookup failed

### POST /update/apply

Downloads and installs the selected release asset to `auto_update.agent_path`. If an update is installed, the agent responds before restarting `auto_update.service_name` through `systemctl`.

**Request Body (JSON, optional):**
```json
{
  "channel": "candidate",
  "force": false
}
```

`force: true` reinstalls the selected release even when the current version already matches.

**Status Codes:**
- `200 OK` — No update was installed
- `202 Accepted` — Update installed; restart scheduled
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Invalid JSON
- `502 Bad Gateway` — Release lookup, download, install, or restart preparation failed

---

### POST /backup/status

Returns the current DN42 BGP/WireGuard backup installation and migration status.

**Request Body:** Empty.

**Response (JSON):**
```json
{
  "enabled": true,
  "installed": true,
  "legacy_detected": false,
  "legacy_migrated": true,
  "node_name": "cn01",
  "git_instance": "https://git.example.com",
  "git_org": "dn42-backup",
  "repo_name": "cn01",
  "repo_url": "https://git.example.com/dn42-backup/cn01",
  "work_dir": "/var/lib/bgp-backup/repo",
  "bird_dir": "/etc/bird",
  "wireguard_dir": "/etc/wireguard",
  "last_sync_at": "2026-08-12T10:00:00Z"
}
```

The response never contains the API token.

**Status Codes:**
- `200 OK` — Always
- `403 Forbidden` — Invalid or missing token

---

### POST /backup/install

Installs or upgrades the Go-based backup service. If the target repository does not exist, it is created and the current `/etc/bird` and `/etc/wireguard` configurations are pushed. If the repository already exists, `existing_repo_action` selects whether to restore the remote configuration or overwrite the remote with local configuration.

**Request Body (JSON):**
```json
{
  "node_name": "cn01",
  "git_instance": "https://git.example.com",
  "git_org": "dn42-backup",
  "api_token": "forgejo-token",
  "existing_repo_action": "restore"
}
```

`existing_repo_action` may be `restore` or `overwrite` and is required when the repository already exists.

**Response (JSON):**
```json
{
  "installed": true,
  "node_name": "cn01",
  "repo_name": "cn01",
  "repo_url": "https://git.example.com/dn42-backup/cn01",
  "repo_existed": true,
  "action_taken": "restore"
}
```

**Status Codes:**
- `200 OK` — Installed or restored successfully
- `400 Bad Request` — Invalid JSON
- `403 Forbidden` — Invalid or missing token
- `502 Bad Gateway` — API authentication, repository, git, or state-file operation failed

---

### POST /backup/sync

Runs one backup synchronization immediately. The agent snapshots local configuration, commits it, merges remote changes (remote human edits take priority), and pushes the result.

**Request Body:** Empty.

**Response (JSON):**
```json
{
  "synced": true,
  "committed": true,
  "pushed": true,
  "remote_merged": false,
  "message": ""
}
```

**Status Codes:**
- `200 OK` — Sync completed
- `403 Forbidden` — Invalid or missing token
- `502 Bad Gateway` — Backup is not installed or a git/filesystem operation failed

---

### POST /errorlist

Returns a list of all peers with detected issues (config mismatches, stale handshakes, BIRD session errors).

**Request Body:** Empty.

**Response (JSON):**
```json
[
  {
    "asn": 4242421234,
    "issues": [
      "WireGuard handshake stale",
      "BIRD DN42_4242421234_v6 state: Active"
    ]
  },
  {
    "asn": 4242425678,
    "issues": [
      "WireGuard config exists but BIRD config missing"
    ]
  }
]
```

Possible issue messages:
- `WireGuard config exists but BIRD config missing`
- `BIRD config exists but WireGuard config missing`
- `WireGuard status check failed`
- `WireGuard never handshaked`
- `WireGuard handshake stale` (no handshake in 900+ seconds)
- `BIRD config unreadable`
- `BIRD <session> check failed`
- `BIRD <session> state: <state>` (any state other than `Established`)

**Status Codes:**
- `200 OK` — Success (array may be empty if no issues)
- `403 Forbidden` — Invalid or missing token
- `500 Internal Server Error` — Failed to list config directories

**Example:**
```bash
curl -X POST http://agent-host:54321/errorlist \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

---

### POST /listpeers

Returns the sorted union of peer ASNs found in WireGuard and BIRD config directories. This is an agent v2 extension used by the server's `/listpeers` import/export workflow.

**Request Body:** Empty or `{}`.

**Response (JSON):**
```json
{
  "asns": [4242421234, 4242425678]
}
```

| Field | Type | Description |
|-------|------|-------------|
| `asns` | array | Sorted ASN list from `/etc/wireguard/dn42-*.conf` and `/etc/bird/dn42_peers/*.conf` |

**Status Codes:**
- `200 OK` — Success
- `403 Forbidden` — Invalid or missing token
- `500 Internal Server Error` — Failed to list WireGuard or BIRD config directories

**Example:**
```bash
curl -X POST http://agent-host:54321/listpeers \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d '{}'
```

---

## Network Diagnostics

### POST /ping

Runs `ping` (5 packets, 6s timeout) against the specified target. When `dns_servers` is configured in agent-v2, hostnames are resolved through those DNS servers before invoking `ping`.

**Request Body (plain text):**
```
172.20.x.x
```

**Response (plain text):**
```
PING 172.20.x.x (172.20.x.x) 56(84) bytes of data.
64 bytes from 172.20.x.x: icmp_seq=1 ttl=64 time=1.23 ms
...
```

**Status Codes:**
- `200 OK` — Ping output (may include error messages from ping itself)
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `405 Method Not Allowed` — Non-POST method
- `408 Request Timeout` — Ping did not complete within 8 seconds
- `500 Internal Server Error` — ping command failed with no output

**Example:**
```bash
curl -X POST http://agent-host:54321/ping \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "172.20.x.x"
```

---

### POST /trace

Runs the built-in NTrace-core traceroute/MTR engine against the specified target. The server maps `/trace`, `/traceroute`, and `/mtr` commands to this endpoint. When `dns_servers` is configured in agent-v2, hostnames are resolved through those DNS servers instead of the system resolver.

**Request Body (plain text):**
```
172.20.x.x
```

**Response (plain text):**
```
 1  10.0.0.1  1.234 ms
 2  172.20.x.x  5.678 ms
```

Trailing hops that respond with only `*` are counted and appended as a summary (e.g., `3 hops not responding.`).

**Status Codes:**
- `200 OK` — Traceroute output
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `405 Method Not Allowed` — Non-POST method
- `408 Request Timeout` — Built-in traceroute did not complete within 8 seconds
- `500 Internal Server Error` — Built-in traceroute failed, commonly because the process lacks root or `CAP_NET_RAW`

**Example:**
```bash
curl -X POST http://agent-host:54321/trace \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "172.20.x.x"
```

---

### GET /bird, /bird6, /traceroute, /traceroute6

These routes implement the bird-lg-go proxy protocol when `looking_glass.enabled` is true. They use the `q` query parameter and the Looking Glass source policy instead of the management token. `/bird6` is an alias of `/bird`, and `/traceroute6` is an alias of `/traceroute`, matching bird-lg-go v1.4.7 behavior.

`/bird` accepts only the `show protocols` and `show route` BIRD command families. Every BIRD connection enters restricted mode before the query is sent. The traceroute routes use the same built-in NTrace engine as `POST /trace` and can be disabled independently.

```bash
curl 'http://agent-host:54321/bird?q=show%20protocols'
curl 'http://agent-host:54321/traceroute?q=172.20.0.1'
```

Responses are plain text and capped by `looking_glass.max_output_bytes`. BIRD command errors are returned as plain-text results for frontend compatibility.

**Status Codes:**

- `200 OK` — Query output, including BIRD command error text
- `400 Bad Request` — Missing/invalid query or traceroute target
- `403 Forbidden` — Source denied or BIRD command outside the read-only allowlist
- `405 Method Not Allowed` — Non-GET method
- `414 URI Too Long` — Decoded query exceeds the configured limit
- `500 Internal Server Error` — BIRD connection/protocol or traceroute execution failure
- `501 Not Implemented` — Traceroute is disabled
- `503 Service Unavailable` — Configured concurrency limit reached
- `504 Gateway Timeout` — Configured request timeout reached

---

### POST /tcping

Runs the built-in TCPing implementation against the specified target. It sends 5 TCP connect probes using Go's `net.Dialer`; no external `tcping` command is required. When `dns_servers` is configured in agent-v2, hostnames are resolved through those DNS servers instead of the system resolver.

**Request Body (plain text):**
```
172.20.x.x 22
```

**Response (plain text):**
```
Connected to 172.20.x.x:22: seq=1 time=1.23 ms
Connected to 172.20.x.x:22: seq=2 time=1.56 ms
...

Ping statistics for 172.20.x.x:22
 5 probes sent, 5 successful, 0 failed.
```

**Status Codes:**
- `200 OK` — TCPing output
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body or invalid host/port
- `405 Method Not Allowed` — Non-POST method
- `408 Request Timeout` — TCPing did not complete within 10 seconds

**Example:**
```bash
curl -X POST http://agent-host:54321/tcping \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "172.20.x.x 22"
```

---

### POST /route

Queries BIRD for the routing table entry for a target address.

**Request Body (plain text):**
```
fd42:xxxx::2
```

The agent selects the IPv4 or IPv6 BIRD table based on whether the target contains `:`. Queries are sent over the BIRD UNIX control socket (`bird_ctl_path`, default `/var/run/bird/bird.ctl`).

**Response (plain text):** Raw BIRD `show route` output.

**Status Codes:**
- `200 OK` — Route lookup result, including BIRD messages such as `Network not in table` when the control socket returns an error body
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `408 Request Timeout` — BIRD query did not complete within 30 seconds
- `500 Internal Server Error` — BIRD command failed with no output

**Example:**
```bash
curl -X POST http://agent-host:54321/route \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "fd42:xxxx::2"
```

---

### POST /path

Queries BIRD for the AS path to a target address. Returns only the AS path string.

**Request Body (plain text):**
```
fd42:xxxx::2
```

**Response (plain text):** The AS path, e.g., `4242420001 4242420002`.

**Status Codes:**
- `200 OK` — AS path found
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `404 Not Found` — No route found or AS path not present in route attributes (including control-socket error replies without `BGP.as_path`)
- `408 Request Timeout` — BIRD query did not complete within 30 seconds
- `500 Internal Server Error` — BIRD command failed with no output

**Example:**
```bash
curl -X POST http://agent-host:54321/path \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "fd42:xxxx::2"
```

---

## Topology

### POST /igp_topology

Returns the Babel IGP topology: interfaces and neighbors.

**Request Body:** Empty.

**Response (JSON):**
```json
{
  "protocol": "babel",
  "interfaces": [
    {
      "interface": "dn42-4242421234",
      "next_hop_v4": "172.20.x.x",
      "next_hop_v6": "fd42:xxxx::2"
    }
  ],
  "neighbors": [
    {
      "interface": "dn42-4242421234",
      "cost": 128,
      "address": "fe80::xxxx:xxxx:xxxx:xxxx"
    }
  ],
  "errors": []
}
```

| Field | Type | Description |
|-------|------|-------------|
| `protocol` | string | Always `"babel"` |
| `interfaces` | array | Babel interfaces with next-hop addresses |
| `neighbors` | array | Babel neighbors with costs |
| `errors` | array | Error messages if BIRD queries failed |

**Status Codes:**
- `200 OK` — Success (check `errors` array for partial failures)
- `403 Forbidden` — Invalid or missing token

**Example:**
```bash
curl -X POST http://agent-host:54321/igp_topology \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

---

## Status Code Summary

| Code | Meaning |
|------|---------|
| `200` | Request succeeded |
| `400` | Malformed request body or missing required field |
| `403` | Missing or invalid `X-DN42-Bot-Api-Secret-Token` header |
| `404` | Resource not found (peer config, route, etc.) |
| `405` | Non-POST method used on ping/trace/tcping |
| `408` | Command execution timed out |
| `500` | Internal server error (command failure, config I/O, etc.) |
| `503` | Service unavailable (peer limit reached, peering closed) |
