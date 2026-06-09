# Agent API Reference

All endpoints use POST method. The agent listens on the address configured in `config.yaml` (default: `0.0.0.0:8080`).

## Authentication

All endpoints except `/version` require authentication via the `X-DN42-Bot-Api-Secret-Token` header. The token must match the `SECRET` value in the agent's `config.yaml`.

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
curl -X POST http://agent-host:8080/version
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

Available keys: `DEFAULT_MTU`, `OPEN`, `MAX_PEERS`, `MIN_PEER_REQUIREMENT`, `EXTRA_MSG`, `MY_DN42_IPv4_ADDRESS`, `VNSTAT_AUTO_ADD`, `VNSTAT_AUTO_REMOVE`, `BIRD_CTL_PATH`, `BIRD_TABLE_4`, `BIRD_TABLE_6`, `NET_SUPPORT`

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
curl -X POST http://agent-host:8080/config/get \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -H "Content-Type: application/json" \
  -d '{"keys": ["DEFAULT_MTU", "OPEN"]}'
```

To get all fields:
```bash
curl -X POST http://agent-host:8080/config/get \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

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
- `500 Internal Server Error` — Failed to count peers

**Example:**
```bash
curl -X POST http://agent-host:8080/pre_peer \
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
curl -X POST http://agent-host:8080/info \
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
curl -X POST http://agent-host:8080/peer \
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
curl -X POST http://agent-host:8080/remove \
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
curl -X POST http://agent-host:8080/restart \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "4242421234"
```

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
curl -X POST http://agent-host:8080/errorlist \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token"
```

---

## Network Diagnostics

### POST /ping

Runs `ping` (5 packets, 6s timeout) against the specified target.

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
curl -X POST http://agent-host:8080/ping \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "172.20.x.x"
```

---

### POST /trace

Runs `traceroute` against the specified target. Attempts DNS resolution first; if that times out, retries with `-n` (no DNS).

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
- `408 Request Timeout` — Traceroute did not complete within 16 seconds (two 8s attempts)
- `500 Internal Server Error` — traceroute failed with no output

**Example:**
```bash
curl -X POST http://agent-host:8080/trace \
  -H "X-DN42-Bot-Api-Secret-Token: my-secret-token" \
  -d "172.20.x.x"
```

---

### POST /tcping

Runs `tcping` (5 probes, no color) against the specified target.

**Request Body (plain text):**
```
172.20.x.x 22
```

**Response (plain text):**
```
172.20.x.x 22 port open, 1.234 sec
172.20.x.x 22 port open, 1.567 sec
...
```

Trailing noise lines matching `Ping stopped.` or `Ping interrupted.` are stripped.

**Status Codes:**
- `200 OK` — TCPing output
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `405 Method Not Allowed` — Non-POST method
- `408 Request Timeout` — TCPing did not complete within 10 seconds
- `500 Internal Server Error` — tcping command failed with no output

**Example:**
```bash
curl -X POST http://agent-host:8080/tcping \
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

The agent selects the IPv4 or IPv6 BIRD table based on whether the target contains `:`.

**Response (plain text):** Raw BIRD `show route` output.

**Status Codes:**
- `200 OK` — Route lookup result
- `403 Forbidden` — Invalid or missing token
- `400 Bad Request` — Empty body
- `408 Request Timeout` — BIRD query did not complete within 30 seconds
- `500 Internal Server Error` — BIRD command failed

**Example:**
```bash
curl -X POST http://agent-host:8080/route \
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
- `404 Not Found` — No route found or AS path not present in route attributes
- `408 Request Timeout` — BIRD query did not complete within 30 seconds
- `500 Internal Server Error` — BIRD command failed

**Example:**
```bash
curl -X POST http://agent-host:8080/path \
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
curl -X POST http://agent-host:8080/igp_topology \
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
