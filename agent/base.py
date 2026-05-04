import json
import os
import re
import threading
import urllib.request
from ipaddress import ip_address
from tools import simple_run

from aiohttp import web

AGENT_VERSION = 29

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
    SERVER_URL = raw_config.get("SERVER_URL")
except BaseException:
    print("Failed to load config file. Exiting.")
    exit(1)


def _is_dns_error(output):
    """Check if wg-quick output indicates a DNS resolution failure."""
    if not output:
        return False
    lower = output.lower()
    return any(
        keyword in lower
        for keyword in (
            "name or service not known",
            "temporary failure in name resolution",
            "no address associated with hostname",
            "nodename nor servname provided",
            "non-recoverable failure in name resolution",
            "could not resolve",
        )
    )


def _has_hostname_endpoint(config_path):
    """Check if a WireGuard config has a hostname-based (non-IP) Endpoint."""
    try:
        with open(config_path, "r") as f:
            for line in f:
                if line.strip().startswith("Endpoint"):
                    value = line.split("=", 1)[1].strip()
                    host = value.rsplit(":", 1)[0].strip().strip("[]")
                    try:
                        ip_address(host)
                        return False
                    except ValueError:
                        return True
    except OSError:
        pass
    return False


def _remove_endpoint_from_config(config_path):
    """Remove the Endpoint line from a WireGuard config file."""
    try:
        with open(config_path, "r") as f:
            content = f.read()
        new_content = re.sub(r"^Endpoint\s*=.*\n?", "", content, flags=re.MULTILINE)
        with open(config_path, "w") as f:
            f.write(new_content)
        return True
    except OSError:
        return False


def _notify_server_dns_failures(failures):
    """POST a list of ASN/endpoint pairs to the server's broadcast endpoint."""
    if not SERVER_URL or not failures:
        return
    try:
        url = SERVER_URL.rstrip("/") + "/internal/broadcast"
        data = json.dumps({"type": "dns_failure", "failures": failures}).encode("utf-8")
        req = urllib.request.Request(
            url,
            data=data,
            headers={
                "Content-Type": "application/json",
                "X-DN42-Bot-Api-Secret-Token": SECRET,
            },
            method="POST",
        )
        urllib.request.urlopen(req, timeout=10)
    except Exception:
        pass


def ensure_wg_interfaces_up():
    """On startup, scan /etc/wireguard and ensure dn42-* interfaces are up.

    This is intended for container environments where systemd is not managing
    wg-quick@ units. It compares existing configs with `wg show` output and
    brings up any missing interfaces.

    If an interface fails to start due to an unresolvable Endpoint, the
    Endpoint is removed from the config and the interface is retried. A
    notification is sent to the server for admin broadcast.
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

    dns_failures = []
    lock = threading.Lock()

    def _up(iface_name):
        config_path = configs[iface_name]
        try:
            output = simple_run(f"wg-quick up {iface_name}", timeout=10)
        except Exception:
            return
        # Verify the interface actually failed to come up
        try:
            current = simple_run("wg show interfaces")
            if iface_name in (current.split() if current else []):
                return  # interface is up, no issue
        except Exception:
            pass
        # Check if failure was caused by DNS resolution
        if _is_dns_error(output) and _has_hostname_endpoint(config_path):
            asn = iface_name[5:] if iface_name.startswith("dn42-") else iface_name
            endpoint = None
            try:
                with open(config_path, "r") as f:
                    for line in f:
                        if line.strip().startswith("Endpoint"):
                            endpoint = line.split("=", 1)[1].strip()
                            break
            except OSError:
                pass
            if _remove_endpoint_from_config(config_path):
                try:
                    simple_run(f"wg-quick up {iface_name}", timeout=10)
                except Exception:
                    pass
                with lock:
                    dns_failures.append({"asn": asn, "endpoint": endpoint})

    from concurrent.futures import ThreadPoolExecutor

    with ThreadPoolExecutor(max_workers=3) as executor:
        executor.map(_up, to_start)

    if dns_failures:
        _notify_server_dns_failures(dns_failures)

routes = web.RouteTableDef()
