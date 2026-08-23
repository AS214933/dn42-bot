# Agent Configuration Reference

The agent reads its configuration from a YAML file. By default, it looks for `config.yaml` in the working directory. Set the `CONFIG_PATH` environment variable to override this.

## Full Example

```yaml
host: "0.0.0.0"
port: 54321
secret: "secret_token"
open: true
max_peers: 0
min_peer_requirement: 0

net_support:
  ipv4: true
  ipv6: true
  ipv4_nat: false
  cn: false

extra_msg: ""
my_dn42_link_local_address: "fe80::1816"
my_dn42_ula_address: "fd2c:1323:4042::1"
my_dn42_ipv4_address: "172.23.246.1"
my_wg_public_key: "LUwqKS6QrCPv510Pwt1eAIiHACYDsbMjrkrbGTJfviU="
sentry_dsn: ""
bird_ctl_path: "/var/run/bird/bird.ctl"
bird_table_4: "master4"
bird_table_6: "master6"
vnstat_auto_add: true
vnstat_auto_remove: false
default_mtu: 1420
server_url: ""
dns_servers: []
looking_glass:
  enabled: false
  allowed_cidrs: []
  disallowed_cidrs: []
  traceroute_enabled: true
  bird_max_concurrent: 16
  traceroute_max_concurrent: 10
  request_timeout: "15s"
  max_query_length: 4096
  max_output_bytes: 65536
peerfinder:
  enabled: false
  host: "::"
  port: 9000
  secret_key: ""
  # secret_key_file: "/etc/dn42-agent/peerfinder.key"
backup:
  enabled: false
  state_file: "/etc/dn42-agent/backup.yaml"
  work_dir: "/var/lib/dn42-agent/backup"
  bird_dir: "/etc/bird"
  wireguard_dir: "/etc/wireguard"
  interval: "5m"
  on_boot_delay: "3m"
  random_delay: "30s"
  # Unattended bootstrap (all four fields required together):
  # node_name: "cn01"
  # git_instance: "https://git.example.com"
  # git_org: "dn42-backup"
  # api_token: ""
auto_update:
  enabled: false
  channel: "candidate"
  check_interval: "24h"
  repository: "AS214933/dn42-bot"
  data_dir: "/etc/dn42-agent"
  agent_path: "/etc/dn42-agent/agent"
  service_name: "dn42-agent.service"
  service_path: "/etc/systemd/system/dn42-agent.service"
```

## Top-Level Keys

| Key | Type | Default | Required | Description |
|-----|------|---------|----------|-------------|
| `host` | string | `"0.0.0.0"` | No | Address the agent listens on. |
| `port` | int | `54321` | No | TCP port for the agent API. |
| `secret` | string | `""` | Yes | Shared token used to authenticate requests from the server. Must match the server's `API_TOKEN`. |
| `open` | bool | `true` | No | Whether the agent accepts new peer requests. Set to `false` to stop accepting peers. |
| `max_peers` | int | `0` | No | Maximum number of peers allowed. `0` means unlimited. The agent rejects new peer requests once this limit is reached. |
| `min_peer_requirement` | int | `0` | No | Minimum number of peers the requester must have elsewhere before they can peer with this node. `0` disables the check. |
| `extra_msg` | string | `""` | No | Extra message displayed in peer info responses. Useful for maintenance notices or peering policies. |
| `my_dn42_link_local_address` | string | `""` | Yes | The DN42 IPv6 link-local address of this agent (e.g., `fe80::1816`). Must be a valid IPv6 address. |
| `my_dn42_ula_address` | string | `""` | Yes | The DN42 IPv6 ULA address of this agent (e.g., `fd2c:1323:4042::1`). Must be a valid IPv6 address. |
| `my_dn42_ipv4_address` | string | `""` | Yes | The DN42 IPv4 address of this agent (e.g., `172.23.246.1`). Must be a valid IPv4 address. |
| `my_wg_public_key` | string | `""` | Yes | The WireGuard public key for this agent. Peers use this to establish tunnels. |
| `sentry_dsn` | string | `""` | No | Sentry DSN for error tracking. Leave empty to disable. |
| `bird_ctl_path` | string | `"/var/run/bird/bird.ctl"` | No | Path to the BIRD UNIX control socket. Used for all BIRD queries (route/path/info/topology/looking glass) and configure/restart. |
| `bird_table_4` | string | `""` | Yes | Name of the BIRD routing table for IPv4 routes. |
| `bird_table_6` | string | `""` | Yes | Name of the BIRD routing table for IPv6 routes. |
| `vnstat_auto_add` | bool | `true` | No | Whether to automatically add tunnel interfaces to vnstat for traffic monitoring. |
| `vnstat_auto_remove` | bool | `false` | No | Whether to automatically remove tunnel interfaces from vnstat when peers are deleted. See [conditional logic](#conditional-logic) below. |
| `default_mtu` | int | `1420` | No | Default MTU for WireGuard tunnels. Peers can override this per-peer. |
| `server_url` | string | `""` | No | URL of the server this agent reports to. |
| `dns_servers` | list of strings | `[]` | No | DNS servers used by built-in `ping`, `trace`, and `tcping` hostname resolution. Accepts `IP`, `IP:port`, or `[IPv6]:port`. Empty list uses system DNS. |
| `looking_glass` | object | See below | No | Embedded bird-lg-go proxy-compatible read-only looking glass. |
| `peerfinder` | object | See below | No | Optional DN42 Peer Finder measurement agent on a separate TCP listener. |
| `backup` | object | See below | No | Optional DN42 BGP/WireGuard config backup and seamless migration from the legacy shell installer. |
| `auto_update` | object | See below | No | GitHub release update settings for the bare-metal agent binary. |

## `net_support` Sub-Fields

The `net_support` block describes which network types this agent supports. All fields are booleans.

| Field | Default | Description |
|-------|---------|-------------|
| `ipv4` | `false` | Whether this agent supports IPv4 peering. |
| `ipv6` | `false` | Whether this agent supports IPv6 peering. |
| `ipv4_nat` | `false` | Whether the IPv4 address is behind NAT. If `true`, peers are warned that reachability may be limited. |
| `cn` | `false` | Whether peering with Chinese Mainland networks is allowed. |

## Conditional Logic

A few config fields interact with each other in non-obvious ways:

### `vnstat_auto_remove`

This field is only respected when `vnstat_auto_add` is `true`. If `vnstat_auto_add` is `false`, `vnstat_auto_remove` is forced to `false` regardless of its value in the config file.

### `max_peers`

When `max_peers` is set to `0`, there is no peer limit. The agent accepts all incoming peer requests as long as `open` is `true` and the requester meets `min_peer_requirement`.

### `min_peer_requirement`

When `0`, any user can peer. When set to a positive integer, the requester must already have at least that many peers on other nodes before this agent will accept them.

### `dns_servers`

When this list is non-empty, agent-v2 resolves hostnames for built-in network diagnostics through these DNS servers instead of the system resolver. This affects `/ping`, `/trace`, and `/tcping`. DNS servers are tried in rotating order with fallback to the next configured server on lookup failure.

### `auto_update`

The updater is intended for bare-metal deployments where the operator has already placed the agent data under `/etc/dn42-agent`, installed the agent binary at `/etc/dn42-agent/agent`, and created `/etc/systemd/system/dn42-agent.service`. The agent uses these paths when updating, but does not create the directory, binary path, or systemd unit for you.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Whether the agent periodically checks for and installs newer releases. Manual update endpoints can still be called when this is `false`. |
| `channel` | `"candidate"` | `candidate` includes prereleases such as alpha/beta/rc; `stable` only follows non-prerelease releases. |
| `check_interval` | `"24h"` | Interval for background update checks when `enabled` is true. |
| `repository` | `"AS214933/dn42-bot"` | GitHub repository used for release metadata and assets. |
| `data_dir` | `"/etc/dn42-agent"` | Directory used for temporary update files. Must already exist. |
| `agent_path` | `"/etc/dn42-agent/agent"` | Installed agent binary path to replace during update. |
| `service_name` | `"dn42-agent.service"` | systemd service name restarted after an installed update. |
| `service_path` | `"/etc/systemd/system/dn42-agent.service"` | systemd unit file path checked before restarting. |

### `backup`

When enabled, agent-v2 periodically snapshots `/etc/bird` and `/etc/wireguard` into a private Forgejo/Gitea repository. Use `POST /backup/install` once to provide the node name, Git instance, organization, and API token. The agent stores secrets in a mode-`0600` state file and performs the git/HTTP operations in Go; it does not install the legacy shell script.

Sync is bidirectional and **remote-authoritative**: before committing local samples the agent merges `origin/main` with `-X theirs`, so human edits made through the Git web UI always win conflicts. Whenever remote history moved, the merged content is applied back onto `/etc/bird` and `/etc/wireguard`, BIRD is reloaded over its control socket, and restored WireGuard interfaces that are not yet up are started via `wg-quick up`. The node's own post-merge sample is then committed on top, so genuinely local changes still reach the repository.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Start the backup loop after configuration is present. Legacy installations enable it automatically after migration. |
| `state_file` | `"/etc/dn42-agent/backup.yaml"` | Absolute path where the generated backup state (including the API token) is stored. |
| `work_dir` | `"/var/lib/dn42-agent/backup"` | Absolute path of the local git checkout used for snapshots. |
| `bird_dir` | `"/etc/bird"` | Absolute path sampled into the repository as `bird/`. |
| `wireguard_dir` | `"/etc/wireguard"` | Absolute path sampled into the repository as `wireguard/`. |
| `interval` | `"5m"` | Interval between periodic sync runs. |
| `on_boot_delay` | `"3m"` | Delay before the first sync after the agent starts. |
| `random_delay` | `"30s"` | Random jitter added to the initial sync delay. |
| `node_name` | `""` | Unattended bootstrap: node name. Requires the other three bootstrap fields. |
| `git_instance` | `""` | Unattended bootstrap: Forgejo/Gitea base URL. |
| `git_org` | `""` | Unattended bootstrap: target organization. |
| `api_token` | `""` | Unattended bootstrap: API token with repository read/write access. |

On startup, the agent detects the legacy shell installation at `/etc/bgp-backup/bgp-backup.conf`, `/etc/systemd/system/bgp-backup.{service,timer}`, and `/usr/local/bin/bgp-backup-sync.sh`. It copies the old configuration into `state_file` (including credentials recovered from `REPO_URL`), writes the four bootstrap fields into its own config.yaml `backup:` block (comment-preserving; this is the only case in which agent-v2 ever edits its config file), then stops/disables and removes the old systemd units and sync script.

The same fold-in runs for a pre-existing state file: if `state_file` exists but the config's `backup:` block lacks any of the four bootstrap fields, the values are written into config.yaml and the state file is renamed to `<state_file>.old` (e.g. `/etc/dn42-agent/backup.yaml.old`). The running backup keeps using the in-memory state either way.

**Unattended bootstrap / node migration:** when all four bootstrap fields (`node_name`, `git_instance`, `git_org`, `api_token`) are set in the config and no backup state exists yet, the agent self-installs at startup without a `POST /backup/install` call. The remote repository is always authoritative in this flow: if the repository already exists — for example because you migrated the agent to a new machine while the old node's backups remain on the Git server — its content is pulled back onto `/etc` (restore), BIRD is reloaded, missing WireGuard interfaces are brought up, and only afterwards does periodic sync resume. A missing repository is created from the current local configuration. Partial bootstrap blocks (some fields set but not all) are rejected at config load time.

### `looking_glass`

When enabled, the agent registers `GET /bird`, `/bird6`, `/traceroute`, and `/traceroute6` on its existing API port. These four routes do not use the agent API token because bird-lg-go frontend does not send it; source filtering is therefore mandatory. The BIRD session is always switched to restricted mode and only `show protocols` and `show route` commands are accepted.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Register the four bird-lg-compatible routes. |
| `allowed_cidrs` | `[]` | Allowed source selectors, CIDRs, or individual IPs. An empty list denies all access. |
| `disallowed_cidrs` | `[]` | Denied selectors, CIDRs, or individual IPs. Denials take priority over allowances. |
| `traceroute_enabled` | `true` | Enable the two traceroute aliases using the agent's built-in NTrace engine. |
| `bird_max_concurrent` | `16` | Maximum simultaneous BIRD socket queries. Valid range: 1-256. |
| `traceroute_max_concurrent` | `10` | Maximum simultaneous traceroutes. Valid range: 1-64. |
| `request_timeout` | `"15s"` | Per-request timeout, up to 2 minutes. |
| `max_query_length` | `4096` | Maximum decoded `q` query length. Valid range: 1-4096 bytes. |
| `max_output_bytes` | `65536` | Maximum response body size. Valid range: 1-65536 bytes. |

The four built-in selectors are:

- `any`: every valid IPv4 or IPv6 source.
- `public`: globally routable Internet space excluding private, loopback, link-local, CGNAT, documentation, reserved, and DN42 address space.
- `private`: RFC 1918 IPv4 and `fc00::/7` IPv6 ULA space.
- `dn42`: `172.20.0.0/14`, the project-compatible `172.31.0.0/16` range, and `fd00::/8`.

For a central frontend with fixed egress addresses, explicit `/32` and `/128` entries are preferred. The built-in combinations are:

- Block public Internet: `allowed_cidrs: [any]` with `disallowed_cidrs: [public]`.
- DN42 only: `allowed_cidrs: [dn42]` with an empty deny list.
- Public Internet plus DN42: `allowed_cidrs: [public, dn42]` with an empty deny list.

Source checks use the TCP peer address and do not trust proxy forwarding headers.

### `peerfinder`

When enabled, agent-v2 starts a separate TCP listener compatible with the DN42 Peer Finder measurement agent protocol. It does not share the main HTTP API listener; `peerfinder.port` must be different from the top-level `port`.

Requests and responses use the Peer Finder binary frame format: HMAC-SHA256 over `timestamp || nonce || body`, a 30-second timestamp skew limit, a nonce replay cache, and JSON bodies for the `version` and `ping` commands. The `ping` command only accepts literal IPv4 or IPv6 addresses and returns packet counts plus RTT statistics parsed from `ping -n -q -c 4 -w 6`.

| Field | Default | Description |
|-------|---------|-------------|
| `enabled` | `false` | Start the Peer Finder TCP measurement listener. |
| `host` | `"::"` | IP address to bind. The default attempts a dual-stack IPv6 listener. |
| `port` | `9000` | TCP port for Peer Finder requests. Must not equal the main agent API port. |
| `secret_key` | `""` | 32-byte hex HMAC key returned by Peer Finder registration. Required when enabled unless `secret_key_file` is set. |
| `secret_key_file` | `""` | File containing the same 32-byte hex key. Relative paths are resolved from the config file directory. Takes priority over `secret_key`. |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `CONFIG_PATH` | Path to the YAML config file. Defaults to `config.yaml` in the current directory. |
