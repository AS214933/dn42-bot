import json
import os
from ipaddress import ip_address
from tools import simple_run

from aiohttp import web

AGENT_VERSION = 28

try:
    with open("agent_config.json", "r") as f:
        raw_config = json.load(f)
    HOST = raw_config["HOST"]
    PORT = raw_config["PORT"]
    SECRET = raw_config["SECRET"]
    OPEN = raw_config["OPEN"]
    MAX_PEERS = raw_config["MAX_PEERS"] if raw_config["MAX_PEERS"] > 0 else 0
    MIN_PEER_REQUIREMENT = raw_config["MIN_PEER_REQUIREMENT"] if raw_config["MIN_PEER_REQUIREMENT"] > 0 else 0
    NET_SUPPORT = raw_config["NET_SUPPORT"]
    EXTRA_MSG = raw_config["EXTRA_MSG"]
    MY_DN42_LINK_LOCAL_ADDRESS = ip_address(raw_config["MY_DN42_LINK_LOCAL_ADDRESS"])
    MY_DN42_ULA_ADDRESS = ip_address(raw_config["MY_DN42_ULA_ADDRESS"])
    MY_DN42_IPv4_ADDRESS = ip_address(raw_config["MY_DN42_IPv4_ADDRESS"])
    MY_WG_PUBLIC_KEY = raw_config["MY_WG_PUBLIC_KEY"]
    BIRD_CTL_PATH = raw_config.get("BIRD_CTL_PATH", "/var/run/bird/bird.ctl")
    BIRD_TABLE_4 = raw_config["BIRD_TABLE_4"]
    BIRD_TABLE_6 = raw_config["BIRD_TABLE_6"]
    VNSTAT_AUTO_ADD = raw_config["VNSTAT_AUTO_ADD"]
    VNSTAT_AUTO_REMOVE = raw_config["VNSTAT_AUTO_REMOVE"] if VNSTAT_AUTO_ADD else False
    DEFAULT_MTU = raw_config.get("DEFAULT_MTU", 1420)
    SENTRY_DSN = raw_config["SENTRY_DSN"]
except BaseException:
    print("Failed to load config file. Exiting.")
    exit(1)

def ensure_wg_interfaces_up():
    """On startup, scan /etc/wireguard and ensure dn42-* interfaces are up.

    This is intended for container environments where systemd is not managing
    wg-quick@ units. It compares existing configs with `wg show` output and
    brings up any missing interfaces.
    """

    try:
        configs = {
            f[:-5]: os.path.join("/etc/wireguard", f)
            for f in os.listdir("/etc/wireguard")
            if f.startswith("dn42-") and f.endswith(".conf")
        }
    except FileNotFoundError:
        return

    if not configs:
        return

    out = simple_run("wg show interfaces")
    existing = set(out.split()) if out else set()

    to_start = [name for name in configs if name not in existing]
    if not to_start:
        return

    from concurrent.futures import ThreadPoolExecutor

    def _up(iface_name):
        try:
            simple_run(f"wg-quick up {iface_name}", timeout=10)
        except Exception:
            pass

    with ThreadPoolExecutor(max_workers=3) as executor:
        executor.map(_up, to_start)

routes = web.RouteTableDef()
