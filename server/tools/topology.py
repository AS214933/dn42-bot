import os
import shutil
import subprocess
import tempfile

import base
import config


def get_topology_graph(servers=None):
    """Collect IGP topology from all agents and render a graphviz PNG.

    Args:
        servers: dict of {region_key: display_name}. Defaults to base.servers.

    Returns:
        Path to the generated PNG file, or None on failure.
    """
    if servers is None:
        servers = base.servers

    from tools.tools import get_from_agent

    api_result = get_from_agent("igp_topology", "", server=servers, timeout=10, retry=2)

    my_asn = getattr(config, "DN42_ASN", 0)
    all_neighbors = []

    for region, (text, status) in api_result.items():
        if status != 200:
            continue
        try:
            data = text if isinstance(text, dict) else __import__("json").loads(text)
        except Exception:
            continue
        if not isinstance(data, dict):
            continue
        for n in data.get("neighbors", []):
            all_neighbors.append({
                "server": region,
                "peer_asn": n.get("peer_asn"),
                "cost": n.get("cost", 65535),
            })

    if not all_neighbors:
        return None

    return _render_graph(servers, my_asn, all_neighbors)


def _render_graph(servers, my_asn, all_neighbors):
    """Render a network topology graph using graphviz DOT language.

    Returns path to generated PNG, or None if graphviz is not available.
    """
    dot_path = shutil.which("dot")
    if not dot_path:
        return None

    # Collect all unique ASNs
    all_asns = set()
    for n in all_neighbors:
        all_asns.add(my_asn)
        if n["peer_asn"]:
            all_asns.add(n["peer_asn"])

    # Short labels for our servers
    short_names = {}
    for key, display in servers.items():
        parts = display.split("|")
        short_names[key] = parts[0].strip() if parts else key

    # Build edges (deduplicate by sorted pair)
    edges = {}
    for n in all_neighbors:
        if not n["peer_asn"]:
            continue
        edge_key = tuple(sorted([my_asn, n["peer_asn"]]))
        if edge_key not in edges or n["cost"] < edges[edge_key]:
            edges[edge_key] = n["cost"]

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

    # Our server nodes (highlighted)
    for key, display in servers.items():
        label = f"{short_names[key]}\\nAS{my_asn}"
        lines.append(f'    "{my_asn}_{key}" [label="{label}", fillcolor="#4682B4", fontcolor="white"];')

    # Peer nodes
    peer_asns = set()
    for n in all_neighbors:
        if n["peer_asn"] and n["peer_asn"] != my_asn:
            peer_asns.add(n["peer_asn"])
    for asn in sorted(peer_asns):
        lines.append(f'    "{asn}" [label="AS{asn}", fillcolor="#D3D3D3"];')

    lines.append("")

    # Edges
    for (a, b), cost in sorted(edges.items()):
        penwidth = max(1.0, 3.0 * (1.0 - cost / (max_cost + 1)) + 1.0)
        if a == my_asn:
            # Find which server key connects to this peer
            server_key = None
            for n in all_neighbors:
                if n["peer_asn"] == b:
                    server_key = n["server"]
                    break
            node_a = f"{my_asn}_{server_key}" if server_key else str(my_asn)
            lines.append(f'    "{node_a}" -- "{b}" [label="{cost}", penwidth={penwidth:.1f}];')
        elif b == my_asn:
            server_key = None
            for n in all_neighbors:
                if n["peer_asn"] == a:
                    server_key = n["server"]
                    break
            node_b = f"{my_asn}_{server_key}" if server_key else str(my_asn)
            lines.append(f'    "{a}" -- "{node_b}" [label="{cost}", penwidth={penwidth:.1f}];')
        else:
            lines.append(f'    "{a}" -- "{b}" [label="{cost}", penwidth={penwidth:.1f}, style=dashed, color="#999999"];')

    # Legend
    lines.append("")
    lines.append('    subgraph cluster_legend {')
    lines.append('        label="Legend"; fontsize=10; style=dashed; color="#CCCCCC";')
    lines.append('        legend_internal [label="Our Server", fillcolor="#4682B4", fontcolor="white"];')
    lines.append('        legend_peer [label="Peer Node", fillcolor="#D3D3D3"];')
    lines.append('        legend_internal -- legend_peer [label="IGP cost", penwidth=2.0];')
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
            return None

        if not os.path.isfile(png_file):
            return None

        return png_file
    except Exception:
        return None
