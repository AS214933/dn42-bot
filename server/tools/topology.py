import os
import json
import shutil
import subprocess
import tempfile
from ipaddress import ip_address

import base


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

    from tools.tools import get_from_agent

    api_result = get_from_agent("igp_topology", "", server=servers, timeout=10, retry=2)

    # Parse all regions' babel data
    regions = {}
    for region, (text, status) in api_result.items():
        if status != 200:
            continue
        try:
            data = text if isinstance(text, dict) else json.loads(text)
        except Exception:
            continue
        if not isinstance(data, dict) or data.get("protocol") != "babel":
            continue

        interfaces = data.get("interfaces") or []
        neighbors = data.get("neighbors") or []
        if not isinstance(interfaces, list) or not isinstance(neighbors, list):
            continue

        regions[region] = {"interfaces": interfaces, "neighbors": neighbors}

    if not regions:
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

    # Infer PoP<->PoP edges by matching neighbor address to remote region's interface address.
    edges = {}
    for region, info in regions.items():
        for n in info["neighbors"]:
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
        return None

    return _render_graph(servers, edges)


def _render_graph(servers, edges):
    """Render a PoP mesh graph using graphviz DOT language.

    Returns path to generated PNG, or None if graphviz is not available.
    """
    dot_path = shutil.which("dot")
    if not dot_path:
        return None

    # Short labels for our servers
    short_names = {}
    for key, display in servers.items():
        parts = display.split("|")
        short_names[key] = parts[0].strip() if parts else key

    # Find max cost for line thickness scaling
    max_cost = max(edges.values()) if edges else 1

    # Generate DOT
    lines = [
        "graph topology {",
        '    graph [overlap=false, splines=true, bgcolor="white", pad="0.5"];',
        '    node [shape=box, style="rounded,filled", fillcolor="#B0C4DE", fontname="Helvetica", fontsize=11];',
        '    edge [fontname="Helvetica", fontsize=9];',
        "",
    ]

    # PoP nodes
    for key, display in servers.items():
        label = short_names.get(key, key)
        lines.append(f'    "{key}" [label="{label}", fillcolor="#4682B4", fontcolor="white"];')

    lines.append("")

    # Edges
    for (a, b), cost in sorted(edges.items()):
        penwidth = max(1.0, 3.0 * (1.0 - cost / (max_cost + 1)) + 1.0)
        lines.append(f'    "{a}" -- "{b}" [label="{cost}", penwidth={penwidth:.1f}];')

    # Legend
    lines.append("")
    lines.append('    subgraph cluster_legend {')
    lines.append('        label="Legend"; fontsize=10; style=dashed; color="#CCCCCC";')
    lines.append('        legend_pop [label="PoP", fillcolor="#4682B4", fontcolor="white"];')
    lines.append('        legend_link [label="Babel neighbor metric", shape=plaintext];')
    lines.append('        legend_pop -- legend_link [label="metric", penwidth=2.0];')
    lines.append("    }")

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
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        if not os.path.isfile(png_file):
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        return png_file
    except Exception:
        shutil.rmtree(tmpdir, ignore_errors=True)
        return None
