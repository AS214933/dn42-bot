import os
import re

import base
from aiohttp import web
from tools import set_sentry, simple_run


def _get_igp_protocol():
    """Determine which IGP protocol to query.

    Priority: explicit config > auto-detect (Babel first, then OSPF).
    Returns 'babel', 'ospf', or None.
    """
    proto = getattr(base, "IGP_PROTOCOL", None)
    if proto in ("babel", "ospf"):
        return proto

    # Auto-detect: check if Babel protocol is configured in BIRD
    out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show protocols")
    if out:
        for line in out.splitlines():
            parts = line.split()
            if len(parts) >= 2 and parts[0] != "name":
                # Protocol name check is case-insensitive
                if len(parts) >= 2 and "babel" in parts[0].lower():
                    return "babel"
    # Fallback to OSPF
    return "ospf"


def _parse_babel_neighbors(output):
    """Parse `birdc show babel neighbors` output.

    Returns list of {"iface": str, "cost": int, "address": str}
    """
    neighbors = []
    for line in output.splitlines():
        # Babel neighbor lines start with an IP address (not whitespace)
        if not line or line[0].isspace():
            continue
        parts = line.split()
        if len(parts) < 4:
            continue
        address = parts[0]
        # Find "iface <name>" and "cost <N>"
        iface = None
        cost = None
        i = 1
        while i < len(parts) - 1:
            if parts[i] == "iface" and i + 1 < len(parts):
                iface = parts[i + 1]
                i += 2
            elif parts[i] == "cost" and i + 1 < len(parts):
                try:
                    cost = int(parts[i + 1])
                except ValueError:
                    pass
                i += 2
            else:
                i += 1
        if iface:
            neighbors.append({"iface": iface, "cost": cost or 65535, "address": address})
    return neighbors


def _parse_ospf_neighbors(output):
    """Parse `birdc show ospf neighbors` output.

    Returns list of {"iface": str, "cost": int, "router_id": str}
    """
    neighbors = []
    for line in output.splitlines():
        if not line or line[0].isspace():
            continue
        parts = line.split()
        if len(parts) < 4:
            continue
        router_id = parts[0]
        # Skip header or invalid lines (router_id should look like an IP)
        if not re.match(r"^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}$", router_id):
            continue
        if router_id == "0.0.0.0":
            continue
        iface = None
        cost = None
        i = 1
        while i < len(parts) - 1:
            if parts[i] == "via" and i + 1 < len(parts):
                iface = parts[i + 1]
                i += 2
            elif parts[i] == "cost" and i + 1 < len(parts):
                try:
                    cost = int(parts[i + 1])
                except ValueError:
                    pass
                i += 2
            else:
                i += 1
        if iface:
            neighbors.append({"iface": iface, "cost": cost or 100, "router_id": router_id})
    return neighbors


def _map_wg_interfaces_to_asn():
    """Map WireGuard interface names to ASN by reading config files.

    Returns dict like {"dn42-4242421816": 4242421816, ...}
    """
    mapping = {}
    try:
        for fname in os.listdir("/etc/wireguard"):
            if fname.startswith("dn42-") and fname.endswith(".conf"):
                asn_str = fname[5:-5]
                if asn_str.isdigit():
                    mapping[f"dn42-{asn_str}"] = int(asn_str)
    except FileNotFoundError:
        pass
    return mapping


def _build_ospf_router_id_to_asn():
    """Build OSPF router ID to ASN mapping by reading BIRD peer configs.

    Reads /etc/bird/dn42_peers/{asn}.conf to find the OSPF router ID
    (which is typically the same as the DN42 IPv4 address used as BGP neighbor).
    """
    mapping = {}
    try:
        conf_dir = "/etc/bird/dn42_peers"
        for fname in os.listdir(conf_dir):
            if not fname.endswith(".conf"):
                continue
            asn_str = fname[:-5]
            if not asn_str.isdigit():
                continue
            asn = int(asn_str)
            filepath = os.path.join(conf_dir, fname)
            try:
                with open(filepath, "r") as f:
                    content = f.read()
                # Extract the neighbor address (used as router ID in OSPF)
                m = re.search(r"neighbor\s+([\d.]+)\s", content)
                if m:
                    mapping[m.group(1)] = asn
            except OSError:
                continue
    except FileNotFoundError:
        pass
    return mapping


@base.routes.post("/igp_topology")
@set_sentry
async def get_igp_topology(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)

    protocol = _get_igp_protocol()
    if not protocol:
        return web.json_response({"protocol": None, "neighbors": []})

    wg_map = _map_wg_interfaces_to_asn()

    if protocol == "babel":
        output = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show babel neighbors")
        if not output:
            return web.json_response({"protocol": "babel", "neighbors": []})
        babel_neighbors = _parse_babel_neighbors(output)
        result = []
        for n in babel_neighbors:
            peer_asn = wg_map.get(n["iface"])
            result.append({
                "interface": n["iface"],
                "peer_asn": peer_asn,
                "cost": n["cost"],
                "address": n["address"],
            })
        return web.json_response({"protocol": "babel", "neighbors": result})

    elif protocol == "ospf":
        output = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show ospf neighbors")
        if not output:
            return web.json_response({"protocol": "ospf", "neighbors": []})
        ospf_neighbors = _parse_ospf_neighbors(output)
        router_id_map = _build_ospf_router_id_to_asn()
        result = []
        for n in ospf_neighbors:
            peer_asn = wg_map.get(n["iface"]) or router_id_map.get(n["router_id"])
            result.append({
                "interface": n["iface"],
                "peer_asn": peer_asn,
                "cost": n["cost"],
                "router_id": n["router_id"],
            })
        return web.json_response({"protocol": "ospf", "neighbors": result})
