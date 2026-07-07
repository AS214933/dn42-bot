#!/usr/bin/env python3
"""
Convert an agent v1 JSON config to an agent v2 YAML config.

Usage:
  python3 scripts/convert_agent_v1_to_v2.py agent/agent_config.json agent-v2/config.yaml
  python3 scripts/convert_agent_v1_to_v2.py --dns-server 172.20.0.53
"""

import argparse
import json
import sys
from pathlib import Path


FIELD_MAP = [
    ("HOST", "host", "0.0.0.0"),
    ("PORT", "port", 54321),
    ("SECRET", "secret", None),
    ("OPEN", "open", True),
    ("MAX_PEERS", "max_peers", 0),
    ("MIN_PEER_REQUIREMENT", "min_peer_requirement", 0),
    ("EXTRA_MSG", "extra_msg", ""),
    ("MY_DN42_LINK_LOCAL_ADDRESS", "my_dn42_link_local_address", None),
    ("MY_DN42_ULA_ADDRESS", "my_dn42_ula_address", None),
    ("MY_DN42_IPv4_ADDRESS", "my_dn42_ipv4_address", None),
    ("MY_WG_PUBLIC_KEY", "my_wg_public_key", None),
    ("SENTRY_DSN", "sentry_dsn", ""),
    ("BIRD_CTL_PATH", "bird_ctl_path", "/var/run/bird/bird.ctl"),
    ("BIRD_TABLE_4", "bird_table_4", None),
    ("BIRD_TABLE_6", "bird_table_6", None),
    ("VNSTAT_AUTO_ADD", "vnstat_auto_add", False),
    ("VNSTAT_AUTO_REMOVE", "vnstat_auto_remove", False),
    ("DEFAULT_MTU", "default_mtu", 1420),
    ("SERVER_URL", "server_url", ""),
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Convert agent v1 agent_config.json to agent v2 config.yaml."
    )
    parser.add_argument(
        "input",
        nargs="?",
        default="agent/agent_config.json",
        help="Path to v1 agent_config.json. Default: agent/agent_config.json",
    )
    parser.add_argument(
        "output",
        nargs="?",
        default="agent-v2/config.yaml",
        help="Path to write v2 config.yaml. Default: agent-v2/config.yaml",
    )
    parser.add_argument(
        "--dns-server",
        action="append",
        default=[],
        help="Add a v2 dns_servers entry. May be repeated.",
    )
    parser.add_argument(
        "--auto-update-enabled",
        action="store_true",
        help="Enable agent-v2 automatic release updates in the generated config.",
    )
    parser.add_argument(
        "--auto-update-channel",
        choices=("candidate", "stable"),
        default="candidate",
        help="Update channel. candidate includes prereleases. Default: candidate.",
    )
    parser.add_argument(
        "--auto-update-check-interval",
        default="24h",
        help="Automatic update check interval. Default: 24h.",
    )
    parser.add_argument(
        "--auto-update-repository",
        default="AS214933/dn42-bot",
        help="GitHub release repository owner/name. Default: AS214933/dn42-bot.",
    )
    parser.add_argument(
        "--auto-update-data-dir",
        default="/etc/dn42-agent",
        help="Agent data directory used for update temp files. Default: /etc/dn42-agent.",
    )
    parser.add_argument(
        "--auto-update-agent-path",
        default="/etc/dn42-agent/agent",
        help="Installed agent binary path. Default: /etc/dn42-agent/agent.",
    )
    parser.add_argument(
        "--auto-update-service-name",
        default="dn42-agent.service",
        help="systemd service name to restart after update. Default: dn42-agent.service.",
    )
    parser.add_argument(
        "--auto-update-service-path",
        default="/etc/systemd/system/dn42-agent.service",
        help="systemd service file path checked before restart. Default: /etc/systemd/system/dn42-agent.service.",
    )
    parser.add_argument(
        "--force",
        action="store_true",
        help="Overwrite output if it already exists.",
    )
    return parser.parse_args()


def load_v1_config(path: Path) -> dict:
    try:
        with path.open("r", encoding="utf-8") as f:
            data = json.load(f)
    except FileNotFoundError:
        raise SystemExit(f"input file not found: {path}")
    except json.JSONDecodeError as exc:
        raise SystemExit(f"invalid JSON in {path}: {exc}")

    if not isinstance(data, dict):
        raise SystemExit("v1 config must be a JSON object")
    return data


def require_value(config: dict, key: str, default):
    if key in config:
        return config[key]
    if default is not None:
        return default
    raise SystemExit(f"missing required v1 config key: {key}")


def convert_config(v1: dict, args: argparse.Namespace) -> dict:
    v2 = {}

    for old_key, new_key, default in FIELD_MAP:
        v2[new_key] = require_value(v1, old_key, default)

    net_support = require_value(v1, "NET_SUPPORT", None)
    if not isinstance(net_support, dict):
        raise SystemExit("NET_SUPPORT must be an object")
    v2["net_support"] = {
        "ipv4": bool(net_support.get("ipv4", False)),
        "ipv6": bool(net_support.get("ipv6", False)),
        "ipv4_nat": bool(net_support.get("ipv4_nat", False)),
        "cn": bool(net_support.get("cn", False)),
    }

    v2["max_peers"] = positive_or_zero(v2["max_peers"])
    v2["min_peer_requirement"] = positive_or_zero(v2["min_peer_requirement"])
    v2["default_mtu"] = int(v2["default_mtu"])
    v2["vnstat_auto_remove"] = bool(v2["vnstat_auto_remove"]) if v2["vnstat_auto_add"] else False
    v2["server_url"] = "" if v2["server_url"] is None else v2["server_url"]
    v2["dns_servers"] = args.dns_server
    v2["auto_update"] = {
        "enabled": bool(args.auto_update_enabled),
        "channel": args.auto_update_channel,
        "check_interval": args.auto_update_check_interval,
        "repository": args.auto_update_repository,
        "data_dir": args.auto_update_data_dir,
        "agent_path": args.auto_update_agent_path,
        "service_name": args.auto_update_service_name,
        "service_path": args.auto_update_service_path,
    }

    return v2


def positive_or_zero(value) -> int:
    value = int(value)
    return value if value > 0 else 0


def yaml_scalar(value) -> str:
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        return str(value)
    if value is None:
        return '""'
    text = str(value)
    escaped = text.replace("\\", "\\\\").replace('"', '\\"')
    return f'"{escaped}"'


def render_yaml(config: dict) -> str:
    lines = [
        f"host: {yaml_scalar(config['host'])}",
        f"port: {yaml_scalar(config['port'])}",
        f"secret: {yaml_scalar(config['secret'])}",
        f"open: {yaml_scalar(config['open'])}",
        f"max_peers: {yaml_scalar(config['max_peers'])}",
        f"min_peer_requirement: {yaml_scalar(config['min_peer_requirement'])}",
        "",
        "net_support:",
        f"  ipv4: {yaml_scalar(config['net_support']['ipv4'])}",
        f"  ipv6: {yaml_scalar(config['net_support']['ipv6'])}",
        f"  ipv4_nat: {yaml_scalar(config['net_support']['ipv4_nat'])}",
        f"  cn: {yaml_scalar(config['net_support']['cn'])}",
        "",
        f"extra_msg: {yaml_scalar(config['extra_msg'])}",
        f"my_dn42_link_local_address: {yaml_scalar(config['my_dn42_link_local_address'])}",
        f"my_dn42_ula_address: {yaml_scalar(config['my_dn42_ula_address'])}",
        f"my_dn42_ipv4_address: {yaml_scalar(config['my_dn42_ipv4_address'])}",
        f"my_wg_public_key: {yaml_scalar(config['my_wg_public_key'])}",
        f"sentry_dsn: {yaml_scalar(config['sentry_dsn'])}",
        f"bird_ctl_path: {yaml_scalar(config['bird_ctl_path'])}",
        f"bird_table_4: {yaml_scalar(config['bird_table_4'])}",
        f"bird_table_6: {yaml_scalar(config['bird_table_6'])}",
        f"vnstat_auto_add: {yaml_scalar(config['vnstat_auto_add'])}",
        f"vnstat_auto_remove: {yaml_scalar(config['vnstat_auto_remove'])}",
        f"default_mtu: {yaml_scalar(config['default_mtu'])}",
        f"server_url: {yaml_scalar(config['server_url'])}",
        "",
        "dns_servers:",
    ]

    if config["dns_servers"]:
        lines.extend(f"  - {yaml_scalar(server)}" for server in config["dns_servers"])
    else:
        lines[-1] = "dns_servers: []"

    lines.extend(
        [
            "",
            "auto_update:",
            f"  enabled: {yaml_scalar(config['auto_update']['enabled'])}",
            f"  channel: {yaml_scalar(config['auto_update']['channel'])}",
            f"  check_interval: {yaml_scalar(config['auto_update']['check_interval'])}",
            f"  repository: {yaml_scalar(config['auto_update']['repository'])}",
            f"  data_dir: {yaml_scalar(config['auto_update']['data_dir'])}",
            f"  agent_path: {yaml_scalar(config['auto_update']['agent_path'])}",
            f"  service_name: {yaml_scalar(config['auto_update']['service_name'])}",
            f"  service_path: {yaml_scalar(config['auto_update']['service_path'])}",
        ]
    )

    return "\n".join(lines) + "\n"


def main() -> int:
    args = parse_args()
    input_path = Path(args.input)
    output_path = Path(args.output)

    if output_path.exists() and not args.force:
        raise SystemExit(f"output already exists: {output_path}; use --force to overwrite")

    v1 = load_v1_config(input_path)
    v2 = convert_config(v1, args)
    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(render_yaml(v2), encoding="utf-8")
    print(f"wrote {output_path}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
