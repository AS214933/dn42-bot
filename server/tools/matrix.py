import html
import logging
import os
import re
import shutil
import subprocess
import tempfile
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime
from urllib.parse import urlparse

import requests
from expiringdict import ExpiringDict
from requests.adapters import HTTPAdapter, Retry
from requests_futures.sessions import FuturesSession

import base
import config

logger = logging.getLogger(__name__)

# Matrix PNG cache: 1 minute, 1 entry
_matrix_cache = ExpiringDict(max_len=1, max_age_seconds=60)

_RTT_PATTERNS = [
    re.compile(r"rtt min/avg/max/(?:mdev|stddev) = ([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+) ms"),
    re.compile(r"round-trip min/avg/max/(?:mdev|stddev) = ([0-9.]+)/([0-9.]+)/([0-9.]+)/([0-9.]+) ms"),
    re.compile(r"round-trip min/avg/max = ([0-9.]+)/([0-9.]+)/([0-9.]+) ms"),
]
_TIME_PATTERN = re.compile(r"\\btime=([0-9.]+)\\s*ms\\b")


def _emit_matrix_debug(lines):
    for line in lines:
        try:
            logger.warning("%s", line)
        except Exception:
            pass
        try:
            print(line, flush=True)
        except Exception:
            pass


def _extract_host(raw):
    """Best-effort extraction of hostname/IP from config values.

    Accepts values like:
    - "hkg.domain.tld"
    - "192.0.2.1:54321"
    - "http://hkg.domain.tld:54321/"
    - "[2001:db8::1]:54321"
    """

    try:
        value = str(raw or "").strip()
    except Exception:
        return ""

    if not value:
        return ""

    # URL with scheme
    if "://" in value:
        try:
            parsed = urlparse(value)
            if parsed.hostname:
                return parsed.hostname
        except Exception:
            pass
        # fall through to manual parsing
        value = value.split("://", 1)[-1]

    value = value.strip().strip("/")

    # [IPv6]:port
    if value.startswith("["):
        end = value.find("]")
        if end != -1:
            return value[1:end]

    # host:port (only if single colon and port is numeric)
    if value.count(":") == 1:
        host, port = value.rsplit(":", 1)
        if port.isdigit() and host:
            return host

    return value


def _api_host_for_region(region):
    if region in getattr(config, "HOSTS", {}):
        return _extract_host(config.HOSTS[region])
    return _extract_host(f"{region}.{config.ENDPOINT}")


def _parse_ping_avg_ms(output_text):
    if not output_text:
        return None

    for pat in _RTT_PATTERNS:
        m = pat.search(output_text)
        if m:
            try:
                # group 2 is avg for 4-field patterns; for 3-field, also group 2.
                return float(m.group(2))
            except Exception:
                return None

    # Fallback: average the per-packet times
    times = []
    for m in _TIME_PATTERN.finditer(output_text):
        try:
            times.append(float(m.group(1)))
        except Exception:
            continue
    if times:
        return sum(times) / len(times)

    return None


def _normalize_ip(value):
    try:
        s = str(value or "").strip()
    except Exception:
        return None
    if not s:
        return None
    if s.lower() in ("none", "null"):
        return None
    return s


def _fetch_dn42_targets(session, regions, *, timeout, debug):
    """Fetch per-region DN42 IPv4 addresses from agents.

    Returns mapping:
        region -> {"v4": str|None}
    """

    info = {r: {"v4": None} for r in regions}

    payload = {"keys": ["MY_DN42_IPv4_ADDRESS"]}

    futures = []
    for region in regions:
        host = _api_host_for_region(region)
        if not host:
            debug.append(f"matrix: region {region} has empty api host")
            continue
        url = f"http://{host}:{config.API_PORT}/config/get"
        try:
            fut = session.post(
                url,
                json=payload,
                headers={"X-DN42-Bot-Api-Secret-Token": config.API_TOKEN},
                timeout=min(8, max(3, int(timeout))),
            )
        except Exception as e:
            debug.append(f"matrix: config/get submit failed region={region} err={type(e).__name__}: {e}")
            continue
        fut.region = region
        futures.append(fut)

    for fut in futures:
        region = getattr(fut, "region", None)
        if not region:
            continue
        try:
            resp = fut.result()
            status = resp.status_code
        except requests.exceptions.Timeout:
            status = 408
            resp = None
        except Exception as e:
            status = 500
            resp = None
            debug.append(f"matrix: config/get failed region={region} err={type(e).__name__}: {e}")

        if status != 200 or resp is None:
            continue

        try:
            data = resp.json()
        except Exception:
            continue
        if not isinstance(data, dict):
            continue

        v4 = _normalize_ip(data.get("MY_DN42_IPv4_ADDRESS"))

        info[region] = {"v4": v4}

    missing = [r for r in regions if not info.get(r, {}).get("v4")]
    if missing:
        debug.append(f"matrix: missing DN42 IPv4 targets for regions: {missing}")

    return info


def _choose_dn42_target(src, dst, dn42_info):
    dst_info = dn42_info.get(dst) or {}

    dst_v4 = dst_info.get("v4")

    return dst_v4


def _lerp(a, b, t):
    return int(round(a + (b - a) * t))


def _rgb_to_hex(rgb):
    r, g, b = rgb
    return f"#{r:02x}{g:02x}{b:02x}"


def _is_dark(rgb):
    r, g, b = rgb
    # Perceived luminance
    return (0.299 * r + 0.587 * g + 0.114 * b) < 140


def _color_for_value(value, vmin, vmax):
    """Return (bg_hex, fg_hex) for a latency value."""

    if value is None:
        return "#8b0000", "#ffffff"

    try:
        v = float(value)
    except Exception:
        return "#8b0000", "#ffffff"

    if vmax <= vmin:
        ratio = 0.0
    else:
        ratio = (v - vmin) / (vmax - vmin)
    ratio = max(0.0, min(1.0, ratio))

    # 3-color gradient: green -> yellow -> red
    green = (63, 185, 80)   # #3fb950
    yellow = (210, 153, 34)  # #d29922
    red = (218, 54, 51)     # #da3633

    if ratio < 0.5:
        t = ratio / 0.5
        rgb = (_lerp(green[0], yellow[0], t), _lerp(green[1], yellow[1], t), _lerp(green[2], yellow[2], t))
    else:
        t = (ratio - 0.5) / 0.5
        rgb = (_lerp(yellow[0], red[0], t), _lerp(yellow[1], red[1], t), _lerp(yellow[2], red[2], t))

    bg = _rgb_to_hex(rgb)
    fg = "#ffffff" if _is_dark(rgb) else "#000000"
    return bg, fg


def get_matrix_graph(servers=None, *, timeout=15, retry=1, max_workers=16):
    """Generate and cache an inter-node latency matrix PNG.

    The matrix is directed: cell (src, dst) is avg RTT of src pinging dst.

    Args:
        servers: dict of {region_key: display_name}. Defaults to base.servers.
        timeout: per-request HTTP timeout (seconds).
        retry: HTTP retry count for transient failures.
        max_workers: thread pool size for concurrent HTTP requests.

    Returns:
        Path to generated PNG file, or None on failure.
    """

    if servers is None:
        servers = base.servers

    if not servers:
        return None

    cached = _matrix_cache.get("matrix")
    if cached is not None:
        tmpdir = tempfile.mkdtemp(prefix="dn42_matrix_")
        png_file = os.path.join(tmpdir, "matrix.png")
        with open(png_file, "wb") as f:
            f.write(cached)
        return png_file

    dot_path = shutil.which("dot")
    if not dot_path:
        return None

    regions = list(servers.keys())

    debug = []

    # Build HTTP session for concurrent agent calls
    workers = max(1, min(int(max_workers), max(1, len(regions) * (len(regions) - 1))))
    session = FuturesSession(executor=ThreadPoolExecutor(max_workers=workers))
    session.mount(
        "http://",
        HTTPAdapter(
            max_retries=Retry(
                total=retry,
                backoff_factor=0.1,
                allowed_methods=("GET", "POST"),
            )
        ),
    )

    # Fetch DN42 internal target addresses from agents
    dn42_info = _fetch_dn42_targets(session, regions, timeout=timeout, debug=debug)

    futures = []

    for src in regions:
        src_api = _api_host_for_region(src)
        if not src_api:
            debug.append(f"matrix: src {src} has empty api host")
            continue
        url = f"http://{src_api}:{config.API_PORT}/ping"

        for dst in regions:
            if src == dst:
                continue
            target = _choose_dn42_target(src, dst, dn42_info)
            if not target:
                continue
            try:
                fut = session.post(
                    url,
                    data=target,
                    headers={"X-DN42-Bot-Api-Secret-Token": config.API_TOKEN},
                    timeout=timeout,
                )
            except Exception:
                continue
            fut.src = src
            fut.dst = dst
            futures.append(fut)

    # Initialize matrix with None
    matrix = {src: {dst: None for dst in regions} for src in regions}

    try:
        for fut in futures:
            src = getattr(fut, "src", None)
            dst = getattr(fut, "dst", None)
            try:
                resp = fut.result()
                status = resp.status_code
                text = resp.text
            except requests.exceptions.Timeout:
                status = 408
                text = ""
            except Exception as e:
                status = 500
                text = ""
                debug.append(f"matrix: request failed src={src} dst={dst} err={type(e).__name__}: {e}")

            if not src or not dst:
                continue

            if status != 200:
                # Keep None => ERR
                continue

            avg = _parse_ping_avg_ms(text)
            if avg is None:
                # Keep None => ERR
                continue

            matrix[src][dst] = avg
    finally:
        # Best-effort cleanup: avoid leaking threads on repeated calls.
        try:
            session.close()
        except Exception:
            pass
        try:
            session.executor.shutdown(wait=False)
        except Exception:
            pass

    # Determine color scale from successful values
    values = [matrix[a][b] for a in regions for b in regions if a != b and matrix[a][b] is not None]
    if values:
        vmin = min(values)
        vmax = max(values)
    else:
        vmin, vmax = 0.0, 1.0
        debug.append("matrix: no successful ping values")

    png_path = _render_matrix_png(dot_path, regions, matrix, vmin=vmin, vmax=vmax)
    if not png_path:
        debug.append("matrix: graphviz render failed")
        _emit_matrix_debug(debug)
        return None

    try:
        with open(png_path, "rb") as f:
            _matrix_cache["matrix"] = f.read()
    except Exception:
        pass

    return png_path


def _render_matrix_png(dot_path, regions, matrix, *, vmin, vmax):
    """Render the matrix into a PNG using Graphviz HTML-like labels."""

    # Table cell sizing tuned for typical 6-12 nodes.
    n = len(regions)
    if n <= 8:
        cell_w, cell_h = 62, 34
    elif n <= 12:
        cell_w, cell_h = 54, 32
    else:
        cell_w, cell_h = 46, 30
    header_w = max(54, int(cell_w * 0.9))

    header_bg = "#ffffff"
    diag_bg = "#f2f2f2"

    def td(text, *, bg=None, fg=None, bold=False, width=None, height=None):
        attrs = []
        if bg:
            attrs.append(f'BGCOLOR="{bg}"')
        if width:
            attrs.append(f'WIDTH="{int(width)}"')
        if height:
            attrs.append(f'HEIGHT="{int(height)}"')
        attrs.append('ALIGN="CENTER"')
        attrs.append('VALIGN="MIDDLE"')
        attrs.append('FIXEDSIZE="TRUE"')
        content = html.escape(str(text))
        if bold:
            content = f"<B>{content}</B>"
        font_attrs = 'FACE="Helvetica" POINT-SIZE="12"'
        if fg:
            font_attrs += f' COLOR="{fg}"'
        content = f"<FONT {font_attrs}>{content}</FONT>"
        return f"<TD {' '.join(attrs)}>{content}</TD>"

    rows = []
    # Header row
    header_cells = [td("", bg=diag_bg, width=header_w, height=cell_h)]
    for col in regions:
        header_cells.append(td(col, bg=header_bg, bold=True, width=cell_w, height=cell_h))
    rows.append(f"<TR>{''.join(header_cells)}</TR>")

    # Data rows
    for row_key in regions:
        cells = [td(row_key, bg=header_bg, bold=True, width=header_w, height=cell_h)]
        for col_key in regions:
            if row_key == col_key:
                cells.append(td("-", bg=diag_bg, width=cell_w, height=cell_h))
                continue

            value = matrix.get(row_key, {}).get(col_key)
            if value is None:
                cells.append(td("ERR", bg="#8b0000", fg="#ffffff", bold=True, width=cell_w, height=cell_h))
            else:
                bg, fg = _color_for_value(value, vmin, vmax)
                cells.append(td(f"{value:.1f}", bg=bg, fg=fg, width=cell_w, height=cell_h))

        rows.append(f"<TR>{''.join(cells)}</TR>")

    table_html = (
        f'<TABLE BORDER="0" CELLBORDER="0" CELLSPACING="2" CELLPADDING="2">'
        + "".join(rows)
        + "</TABLE>"
    )

    timestamp = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    dot_lines = [
        "digraph latency_matrix {",
        '    graph [bgcolor="#ffffff", pad="0.6", nodesep="0.2", ranksep="0.2"];',
        '    node [shape=plaintext, fontname="Helvetica"];',
        '    labelloc="t";',
        f'    label="Inter-Node Latency Matrix  |  {timestamp}";',
        '    fontsize=11; fontcolor="#57606a";',
        "",
        "    matrix [label=<",
        f"        {table_html}",
        "    >];",
        "}",
    ]

    dot_content = "\n".join(dot_lines)

    tmpdir = tempfile.mkdtemp(prefix="dn42_matrix_")
    dot_file = os.path.join(tmpdir, "matrix.dot")
    png_file = os.path.join(tmpdir, "matrix.png")

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
            msg = f"matrix: graphviz dot failed rc={result.returncode} stderr={stderr[:500]}"
            try:
                logger.warning("%s", msg)
            except Exception:
                pass
            try:
                print(msg, flush=True)
            except Exception:
                pass
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        if not os.path.isfile(png_file):
            shutil.rmtree(tmpdir, ignore_errors=True)
            return None

        return png_file
    except Exception as e:
        try:
            print(f"matrix: unexpected render exception: {type(e).__name__}: {e}", flush=True)
        except Exception:
            pass
        shutil.rmtree(tmpdir, ignore_errors=True)
        return None
