import os
import json
import logging
import shutil
import subprocess
import tempfile
from ipaddress import ip_address

from expiringdict import ExpiringDict

import base

logger = logging.getLogger(__name__)

# 拓扑图 PNG 缓存，1 分钟过期，最多 1 个条目
_topology_cache = ExpiringDict(max_len=1, max_age_seconds=60)


def _emit_topology_debug(lines):
    for line in lines:
        try:
            logger.warning("%s", line)
        except Exception:
            # Last resort: avoid breaking command flow due to logging issues
            pass

        # Also print to stdout to make sure it shows up in Docker logs even
        # when logging is configured to suppress warnings.
        try:
            print(line, flush=True)
        except Exception:
            pass


def get_topology_graph(servers=None):
    """Collect Babel topology from all agents and render a graphviz PNG.

    Nodes are PoPs (regions like us0/sg0/...), edges are inferred purely from
    `birdc show babel neighbors/interfaces` outputs returned by agents.

    Args:
        servers: dict of {region_key: display_name}. Defaults to base.servers.

    Returns:
        Path to the generated PNG file, or None on failure.
    """
    if servers is None:
        servers = base.servers

    # 缓存命中：直接返回缓存的 PNG
    cached = _topology_cache.get("topology")
    if cached is not None:
        tmpdir = tempfile.mkdtemp(prefix="dn42_topology_")
        png_file = os.path.join(tmpdir, "topology.png")
        with open(png_file, "wb") as f:
            f.write(cached)
        return png_file

    from tools.tools import get_from_agent

    api_result = get_from_agent("igp_topology", "", server=servers, timeout=10, retry=2)

    debug = []

    non_200 = {region: status for region, (text, status) in api_result.items() if status != 200}
    if non_200:
        debug.append(f"topology: non-200 agent responses: {non_200}")
        if any(code == 403 for code in non_200.values()):
            debug.append("topology: got 403; check server API_TOKEN vs agent SECRET")

    # Parse all regions' babel data
    regions = {}
    for region, (text, status) in api_result.items():
        if status != 200:
            continue
        try:
            data = text if isinstance(text, dict) else json.loads(text)
        except Exception:
            debug.append(f"topology: agent {region} returned invalid JSON")
            continue
        if not isinstance(data, dict) or data.get("protocol") != "babel":
            proto = data.get("protocol") if isinstance(data, dict) else None
            debug.append(f"topology: agent {region} protocol mismatch: {proto!r}")
            continue

        interfaces = data.get("interfaces") or []
        neighbors = data.get("neighbors") or []
        if not isinstance(interfaces, list) or not isinstance(neighbors, list):
            continue

        regions[region] = {"interfaces": interfaces, "neighbors": neighbors}
        debug.append(f"topology: agent {region} parsed interfaces={len(interfaces)} neighbors={len(neighbors)}")

        if data.get("errors"):
            debug.append(f"topology: agent {region} reported errors: {data.get('errors')}")

    if not regions:
        debug.append("topology: no usable babel data from agents (200+protocol=babel required)")
        _emit_topology_debug(debug)
        return None

    # Build address -> region mapping from each region's babel interface next-hop addresses.
    addr_to_region = {}
    for region, info in regions.items():
        for iface in info["interfaces"]:
            if not isinstance(iface, dict):
                continue
            for k in ("next_hop_v6", "next_hop_v4"):
                raw = iface.get(k)
                if not raw:
                    continue
                try:
                    addr_obj = ip_address(raw)
                except ValueError:
                    continue
                if addr_obj.is_unspecified:
                    continue
                addr = str(addr_obj)
                if addr in addr_to_region and addr_to_region[addr] != region:
                    # Ambiguous address across regions; ignore during matching.
                    addr_to_region[addr] = None
                else:
                    addr_to_region[addr] = region

    ambiguous_addrs = sum(1 for v in addr_to_region.values() if v is None)
    debug.append(
        f"topology: parsed regions={len(regions)} iface_addrs={len(addr_to_region)} ambiguous={ambiguous_addrs}"
    )

    # Infer PoP<->PoP edges by matching neighbor address to remote region's interface address.
    edges = {}
    neighbor_rows = 0
    for region, info in regions.items():
        for n in info["neighbors"]:
            neighbor_rows += 1
            if not isinstance(n, dict):
                continue
            raw_addr = n.get("address")
            if not raw_addr:
                continue
            try:
                neighbor_obj = ip_address(raw_addr)
            except ValueError:
                continue
            if neighbor_obj.is_unspecified:
                continue

            neighbor_addr = str(neighbor_obj)

            remote = addr_to_region.get(neighbor_addr)
            if not remote or remote == region:
                continue

            try:
                cost = int(n.get("cost", 65535))
            except Exception:
                cost = 65535

            a, b = sorted((region, remote))
            key = (a, b)
            if key not in edges or cost < edges[key]:
                edges[key] = cost

    if not edges:
        debug.append(
            "topology: no edges inferred (neighbor_rows=%d). "
            "Likely neighbor address does not match any remote next_hop_v6/v4 or addresses are ambiguous."
            % neighbor_rows
        )
        _emit_topology_debug(debug)
        return None

    debug.append(f"topology: inferred edges={len(edges)}")
    png_path = _render_graph(servers, edges)
    if not png_path:
        debug.append("topology: graph rendering failed")
        _emit_topology_debug(debug)
    else:
        # 渲染成功后缓存 PNG 字节
        try:
            with open(png_path, "rb") as f:
                _topology_cache["topology"] = f.read()
        except Exception:
            pass
    return png_path


def _render_graph(servers, edges):
    """Render a PoP mesh graph using graphviz DOT language.

    Returns path to generated PNG, or None if graphviz is not available.
    """
    dot_path = shutil.which("dot")
    if not dot_path:
        try:
            print("topology: graphviz 'dot' not found", flush=True)
        except Exception:
            pass
        return None

    # Find max cost among reachable edges for line thickness scaling
    reachable_costs = [c for c in edges.values() if c < 65535]
    max_cost = max(reachable_costs) if reachable_costs else 1

    # Assign each node a distinct color
    neon_colors = ["#58a6ff", "#3fb950", "#f0883e", "#bc8cff", "#f778ba", "#79c0ff", "#56d364", "#d29922"]
    node_color = {}
    for i, key in enumerate(servers):
        node_color[key] = neon_colors[i % len(neon_colors)]

    bg = "#0a0e17"

    def _blend_with_bg(hex_color, alpha=0.35):
        """Blend a hex color toward the background for transparency effect."""
        try:
            r = int(hex_color[1:3], 16)
            g = int(hex_color[3:5], 16)
            b = int(hex_color[5:7], 16)
            br = int(bg[1:3], 16)
            bv = int(bg[3:5], 16)
            bb = int(bg[5:7], 16)
            r = int(br + (r - br) * alpha)
            g = int(bv + (g - bv) * alpha)
            b = int(bb + (b - bb) * alpha)
            return f"#{r:02x}{g:02x}{b:02x}"
        except Exception:
            return hex_color

    # Generate DOT — dark theme, bgp.tools style
    # Nodes are small dots; labels are placed beside them via xlabel.
    lines = [
        "graph topology {",
        f'    graph [overlap="prism", splines="true", bgcolor="{bg}", pad="0.8", nodesep="1.5", ranksep="1.2", outputorder="edgesfirst"];',
        '    node [shape=point, width="0.15", height="0.15"];',
        '    edge [penwidth="1.0"];',
        "",
        '    labelloc="t"; label="IGP Network Topology"; fontsize=13; fontname="Helvetica Bold"; fontcolor="#8b949e";',
        "",
    ]

    # PoP nodes — small colored dot + label beside it
    for i, (key, display) in enumerate(servers.items()):
        color = node_color[key]
        lines.append(f'    "{key}" [xlabel="{display}", fillcolor="{color}", color="{color}"];')

    lines.append("")

    # Edges — skip unreachable (65535), thin lines colored by source node (blended for transparency)
    for (a, b), cost in sorted(edges.items()):
        if cost >= 65535:
            continue
        color = _blend_with_bg(node_color.get(a, "#30363d"))
        lines.append(f'    "{a}" -- "{b}" [color="{color}", penwidth="1.0"];')

    lines.append("}")
    dot_content = "\n".join(lines)

    tmpdir = tempfile.mkdtemp(prefix="dn42_topology_")
    dot_file = os.path.join(tmpdir, "topology.dot")
    png_file = os.path.join(tmpdir, "topology.png")

    try:
        with open(dot_file, "w") as f:
            f.write(dot_content)

        result = subprocess.run(
            [dot_path, "-Tpng", "-o", png_file, dot_file],
            capture_output=True,
            timeout=30,
        )
        if result.returncode != 0:
            try:
                stderr = (result.stderr or b"").decode("utf-8", errors="replace")
            except Exception:
                stderr = "<decode failed>"
            msg = f"topology: graphviz dot failed rc={result.returncode} stderr={stderr[:500]}"
            logger.warning("%s", msg)
            try:
                print(msg, flush=True)
            except Exception:
                pass
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        if not os.path.isfile(png_file):
            try:
                print("topology: graphviz did not produce png output", flush=True)
            except Exception:
                pass
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        return png_file
    except Exception as e:
        try:
            print(f"topology: unexpected render exception: {type(e).__name__}: {e}", flush=True)
        except Exception:
            pass
        shutil.rmtree(tmpdir, ignore_errors=True)
        return None
