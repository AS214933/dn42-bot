# Peer Import/Export Specification

This document defines the JSON format used to import and export peer
configurations between AutoPeer center instances. It is intended as a
reference for anyone building tooling, agents, or alternative frontends
that produce or consume these files.

---

## Export Format (`GET /api/v1/admin/peers/export`)

The export endpoint returns a single JSON document with a wrapper object.
The file is not subject to API versioning — the schema is fixed.

```json
{
  "version": 1,
  "exported_at": "2026-06-07T12:00:00Z",
  "peers": [ ... ]
}
```

| Field        | Type     | Description                                       |
|-------------|----------|---------------------------------------------------|
| `version`    | integer  | Schema version. Currently `1`.                    |
| `exported_at` | string | UTC timestamp in RFC3339 format.                  |
| `peers`      | array    | Array of peer entry objects (defined below).      |

---

## Peer Entry Object

Each element in the `peers` array has the following fields.

```json
{
  "node_id": "node-fra-01",
  "node_name": "Frankfurt 01",
  "remote_asn": 4242420000,
  "remote_pubkey": "abcdEFGH1234ijklMNOP5678qrstUVWX9012yzAB=",
  "remote_endpoint": "203.0.113.10:51820",
  "remote_lla": "fe80::1",
  "contact_email": "admin@example.com",
  "wg_listen_port": 50000,
  "wg_interface_name": "dn42_20000",
  "bgp_proto_name": "dn42_20000",
  "bird_config_filename": "dn42_20000.conf",
  "wg_managed": true,
  "mtu": 1420,
  "wg_preshared_key": "QmFzZTY0UFNLZXhhbXBsZXZhbHVlMTIzNDU2Nzg5MA==",
  "status": "active"
}
```

### Field Definitions

| Field                  | Type           | Required | Import Key | Description |
|-----------------------|----------------|----------|------------|-------------|
| `node_id`             | string         | yes      | **yes**    | Target node identifier. Must reference an existing node. |
| `node_name`           | string         | no       | no         | Human-readable node name. **Export-only** — ignored on import. |
| `remote_asn`          | integer (int64)| yes      | **yes**    | Peer's DN42 ASN. Combined with `node_id` as the unique key. |
| `remote_pubkey`       | string         | yes      | —          | WireGuard public key, Base64-encoded, minimum 40 characters. |
| `remote_endpoint`     | string         | yes      | —          | WireGuard endpoint in `IP:Port` or `[IPv6]:Port` format. |
| `remote_lla`          | string         | yes      | —          | Link-local address. Must be within `fe80::/10`. |
| `contact_email`       | string         | no       | —          | Contact email (resolved from DN42 registry or set manually). |
| `wg_listen_port`      | integer        | yes      | —          | WireGuard listen port on the node (typically `50000 + asn % 10000`). |
| `wg_interface_name`   | string         | yes      | —          | WireGuard interface name (typically `dn42_{asn % 100000}`). |
| `bgp_proto_name`      | string         | no       | —          | BIRD BGP protocol name. Empty string if not applicable. |
| `bird_config_filename`| string         | no       | —          | BIRD config filename. Empty string if not applicable. |
| `wg_managed`          | boolean        | yes      | —          | `true` if the agent manages the WireGuard tunnel automatically. |
| `mtu`                 | integer\|null  | no       | —          | Tunnel MTU. Must be 576–9000 if set. `null` or absent = default. |
| `wg_preshared_key`    | string\|null   | no       | —          | WireGuard pre-shared key (Base64). `null` or absent = none. |
| `status`              | string         | yes      | —          | One of `pending`, `active`, `suspended`, `rejected`. |

### Import Key

Peers are uniquely identified by the combination of **`node_id` + `remote_asn`**.
On import, this pair is used to determine whether to insert a new row or skip /
overwrite an existing one.

---

## Import Format (`POST /api/v1/admin/peers/import`)

The import endpoint accepts **two** input shapes:

### 1. Full export wrapper (round-trip from export)

```json
{
  "version": 1,
  "exported_at": "2026-06-07T12:00:00Z",
  "peers": [
    { "node_id": "node-fra-01", "remote_asn": 4242420000, ... },
    { "node_id": "node-nyc-01", "remote_asn": 4242420001, ... }
  ]
}
```

### 2. Bare JSON array

```json
[
  { "node_id": "node-fra-01", "remote_asn": 4242420000, ... },
  { "node_id": "node-nyc-01", "remote_asn": 4242420001, ... }
]
```

Both forms are accepted. The wrapper's `version` and `exported_at` are
ignored during import — only the `peers` array is processed.

### Query Parameters

| Parameter    | Type   | Default | Description |
|-------------|--------|---------|-------------|
| `overwrite` | string | `"false"` | Set to `"true"` to update existing peers instead of skipping them. |

### Behavior

- `status` defaults to `"pending"` when empty or absent in the input.
- `id` and timestamp fields in the input are ignored (new IDs are generated).
- When `overwrite=false`: existing peers are skipped silently.
- When `overwrite=true`: existing peers have all mutable columns updated.
- Invalid entries do **not** abort the import; they are reported in `errors`.

### Response

```json
{
  "status": "completed",
  "imported": 3,
  "overwritten": 0,
  "skipped": 1,
  "total": 5,
  "errors": [
    { "node_id": "bogus-node", "asn": 4242420001, "reason": "node not found" }
  ]
}
```

Possible `reason` values: `missing node_id`, `invalid remote_asn`,
`node not found`, `db error: <message>`.

---

## Agent-Based Import (`POST /api/v1/admin/nodes/{id}/import`)

This endpoint asks a live node agent to scan its WireGuard and BIRD
configuration and return the peers it finds. The agent response uses
a **different** schema than the JSON export — it contains only the fields
the agent can observe locally.

### Agent Response Shape

```json
{
  "peers": [
    {
      "asn": 4242420000,
      "remote_pubkey": "abcdEFGH1234ijklMNOP5678qrstUVWX9012yzAB=",
      "remote_endpoint": "203.0.113.10:51820",
      "remote_lla": "fe80::1",
      "wg_listen_port": 50000,
      "wg_interface_name": "dn42_20000",
      "bgp_proto_name": "dn42_20000",
      "bird_config_filename": "dn42_20000.conf",
      "wg_managed": true
    }
  ]
}
```

| Field                  | Type    | Description |
|-----------------------|---------|-------------|
| `asn`                  | integer | Peer ASN (note: **not** `remote_asn` — the agent uses a shorter name). |
| `remote_pubkey`        | string  | WireGuard public key. |
| `remote_endpoint`      | string  | WireGuard endpoint. |
| `remote_lla`           | string  | Link-local address. |
| `wg_listen_port`       | integer | Listen port. |
| `wg_interface_name`    | string  | WireGuard interface name. |
| `bgp_proto_name`       | string  | BIRD BGP protocol name (empty if `wg_managed`). |
| `bird_config_filename` | string  | BIRD config filename (empty if `wg_managed`). |
| `wg_managed`           | boolean | Whether the tunnel is agent-managed. |

**Key differences from the JSON export format:**

- Uses `asn` instead of `remote_asn`.
- No `node_id` (implicit — the node is identified by the URL path).
- No `contact_email`, `status`, `mtu`, or `wg_preshared_key`.
- `wg_managed` is inferred: if both `bgp_proto_name` and
  `bird_config_filename` are empty, it defaults to `true`.

The center resolves `contact_email` from the DN42 registry after receiving
the agent response. All imported peers are inserted as `active` with
duplicate-key `(node_id, remote_asn)` silently skipped (no overwrite mode).

### Center Response

```json
{
  "status": "completed",
  "inserted": 5,
  "skipped": 2,
  "db_errors": 0,
  "total": 7,
  "missing_email": [
    {
      "peer_id": "9f1c2e7a-4b6d-4f0a-9c33-1a2b3c4d5e6f",
      "asn": 4242420001,
      "reason": "registry lookup failed"
    }
  ]
}
```

---

## Complete Example File

A full export file with four peers covering common scenarios:

```json
{
  "version": 1,
  "exported_at": "2026-06-07T12:00:00Z",
  "peers": [
    {
      "node_id": "node-fra-01",
      "node_name": "Frankfurt 01",
      "remote_asn": 4242420000,
      "remote_pubkey": "abcdEFGH1234ijklMNOP5678qrstUVWX9012yzAB=",
      "remote_endpoint": "203.0.113.10:51820",
      "remote_lla": "fe80::1",
      "contact_email": "admin@example.com",
      "wg_listen_port": 50000,
      "wg_interface_name": "dn42_20000",
      "bgp_proto_name": "dn42_20000",
      "bird_config_filename": "dn42_20000.conf",
      "wg_managed": true,
      "mtu": 1420,
      "wg_preshared_key": "QmFzZTY0UFNLZXhhbXBsZXZhbHVlMTIzNDU2Nzg5MA==",
      "status": "active"
    },
    {
      "node_id": "node-fra-01",
      "node_name": "Frankfurt 01",
      "remote_asn": 4242420001,
      "remote_pubkey": "XYZWvuts9876rqpoNMLK5432jihgFEDC1098zyxw=",
      "remote_endpoint": "203.0.113.11:51820",
      "remote_lla": "fe80::2",
      "contact_email": "peer@example.com",
      "wg_listen_port": 50001,
      "wg_interface_name": "dn42_20001",
      "bgp_proto_name": "dn42_20001",
      "bird_config_filename": "dn42_20001.conf",
      "wg_managed": true,
      "mtu": null,
      "status": "pending"
    },
    {
      "node_id": "node-nyc-01",
      "node_name": "New York 01",
      "remote_asn": 4242420002,
      "remote_pubkey": "MNBVCXzasdfghjklPOIUYTREWQ0987654321mnb=",
      "remote_endpoint": "[2001:db8::1]:51820",
      "remote_lla": "fe80::3",
      "contact_email": "admin@example.org",
      "wg_listen_port": 50002,
      "wg_interface_name": "dn42_20002",
      "bgp_proto_name": "",
      "bird_config_filename": "",
      "wg_managed": true,
      "mtu": 1400,
      "status": "active"
    },
    {
      "node_id": "node-lon-01",
      "node_name": "London 01",
      "remote_asn": 4242420003,
      "remote_pubkey": "QWERTYUIOPASDFGHJKLZXCVBNM1234567890qwerty=",
      "remote_endpoint": "198.51.100.20:51820",
      "remote_lla": "fe80::4",
      "contact_email": "contact@example.net",
      "wg_listen_port": 50003,
      "wg_interface_name": "dn42_20003",
      "bgp_proto_name": "dn42_20003_v6",
      "bird_config_filename": "dn42_20003.conf",
      "wg_managed": false,
      "status": "suspended"
    }
  ]
}
```

This example demonstrates:
- Active peer with PSK and custom MTU
- Pending peer with null MTU (uses default)
- Active IPv6 endpoint with agent-managed tunnel (`bgp_proto_name` empty)
- Suspended peer with manual BIRD management (`wg_managed: false`)

---

## Notes for Implementers

1. **Import is DB-only.** JSON import writes rows to the database but does
   not push WireGuard/BIRD configuration to agents. Peers imported as
   `active` must be manually synced or re-approved to take effect on nodes.

2. **The unique key is `(node_id, remote_asn)`, not ASN alone.** A single
   ASN can have peers on multiple nodes, but only one peer per node.

3. **`node_name` is decorative.** It is populated on export as a convenience
   but is never read on import. Always key on `node_id`.

4. **Validation is per-field, not per-entry.** An entry with an invalid
   `remote_asn` or missing `node_id` is added to the `errors` list;
   the remaining entries are still processed.

5. **`wg_managed` inference.** When the agent reports a peer with both
   `bgp_proto_name` and `bird_config_filename` empty, the center treats it
   as agent-managed (`wg_managed = true`).

6. **Schema evolution.** The `version` field in the export wrapper exists
   for future format changes. Currently only version `1` is defined.
   Consumers should reject or migrate if they encounter an unknown version.
