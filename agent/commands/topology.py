from ipaddress import ip_address

import base
from aiohttp import web
from tools import set_sentry, simple_run


def _parse_babel_neighbors(output):
    """Parse `birdc show babel neighbors` output.

    BIRD output is a table (see BIRD source `babel_show_neighbors()`):
    IP address | Interface | Metric | ...

    Returns list of {"interface": str, "cost": int, "address": str}
    """
    neighbors = []
    for line in output.splitlines():
        line = line.strip()
        if not line or line.endswith(":"):
            continue
        parts = line.split()
        if len(parts) < 3:
            continue

        address = parts[0]
        try:
            address = str(ip_address(address))
        except ValueError:
            continue

        iface = parts[1]
        try:
            cost = int(parts[2])
        except ValueError:
            cost = 65535

        neighbors.append({"interface": iface, "cost": cost, "address": address})
    return neighbors


def _parse_babel_interfaces(output):
    """Parse `birdc show babel interfaces` output.

    BIRD output is a table (see BIRD source `babel_show_interfaces()`):
    Interface | State | Auth | RX cost | Nbrs | Timer | Next hop (v4) | Next hop (v6)

    Returns list of {"interface": str, "next_hop_v4": str|None, "next_hop_v6": str|None}
    """
    interfaces = []
    for line in output.splitlines():
        line = line.strip()
        if not line or line.endswith(":"):
            continue

        parts = line.split()
        # Expect at least 8 columns; ignore header lines.
        if len(parts) < 8 or parts[0].lower() == "interface":
            continue

        iface = parts[0]
        next_hop_v4_raw = parts[-2]
        next_hop_v6_raw = parts[-1]

        next_hop_v4 = None
        next_hop_v6 = None
        try:
            next_hop_v4 = str(ip_address(next_hop_v4_raw))
        except ValueError:
            next_hop_v4 = None
        try:
            next_hop_v6 = str(ip_address(next_hop_v6_raw))
        except ValueError:
            next_hop_v6 = None

        interfaces.append({
            "interface": iface,
            "next_hop_v4": next_hop_v4,
            "next_hop_v6": next_hop_v6,
        })
    return interfaces


@base.routes.post("/igp_topology")
@set_sentry
async def get_igp_topology(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)

    # Babel-only: build PoP mesh strictly from `birdc show babel ...` outputs.
    errors = []
    try:
        iface_out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show babel interfaces")
    except Exception as e:
        iface_out = ""
        errors.append(f"show babel interfaces failed: {type(e).__name__}: {e}")

    try:
        neigh_out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show babel neighbors")
    except Exception as e:
        neigh_out = ""
        errors.append(f"show babel neighbors failed: {type(e).__name__}: {e}")

    interfaces = _parse_babel_interfaces(iface_out) if iface_out else []
    neighbors = _parse_babel_neighbors(neigh_out) if neigh_out else []

    if errors:
        try:
            print(f"igp_topology: birdc errors (ctl={base.BIRD_CTL_PATH}): {errors}", flush=True)
        except Exception:
            pass
    elif not interfaces and not neighbors:
        try:
            print(
                f"igp_topology: babel data empty (ctl={base.BIRD_CTL_PATH}); "
                "check babel is configured and has neighbors",
                flush=True,
            )
        except Exception:
            pass

    return web.json_response({
        "protocol": "babel",
        "interfaces": interfaces,
        "neighbors": neighbors,
        "errors": errors,
    })
