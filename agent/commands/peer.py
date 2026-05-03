import os
import re
import time
from ipaddress import IPv4Network, IPv6Network, ip_address

import base
from aiohttp import web
from tools import set_sentry, simple_run

WG_HANDSHAKE_STALE_SECONDS = 900  # 15 minutes


def get_current_peer_num():
    wg_conf = [i[5:-5] for i in os.listdir("/etc/wireguard") if i.startswith("dn42-") and i.endswith(".conf")]
    bird_conf = [i[:-5] for i in os.listdir("/etc/bird/dn42_peers") if i.endswith(".conf")]
    wg_conf_len = len([i for i in wg_conf if i.isdigit()])
    bird_conf_len = len([i for i in bird_conf if i.isdigit()])
    if wg_conf_len != bird_conf_len:
        return None
    else:
        return wg_conf_len


@base.routes.post("/pre_peer")
@set_sentry
async def pre_peer(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)
    current_peer_num = get_current_peer_num()
    if current_peer_num is None:
        return web.Response(body="wireguard and bird config not match", status=500)
    return web.json_response(
        {
            "existed": current_peer_num,
            "max": base.MAX_PEERS,
            "requirement": base.MIN_PEER_REQUIREMENT,
            "open": base.OPEN,
            "net_support": base.NET_SUPPORT,
            "lla": str(base.MY_DN42_LINK_LOCAL_ADDRESS),
            "msg": base.EXTRA_MSG,
        }
    )


@base.routes.post("/info")
@set_sentry
async def get_info(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret == base.SECRET:
        try:
            asn = int(await request.text())
        except BaseException:
            return web.Response(status=400)
    else:
        return web.Response(status=403)

    wg_exist = os.path.isfile(f"/etc/wireguard/dn42-{asn}.conf")
    bird_exist = os.path.isfile(f"/etc/bird/dn42_peers/{asn}.conf")
    if not wg_exist and not bird_exist:
        return web.Response(status=404)
    elif wg_exist and not bird_exist:
        return web.Response(body="wg only", status=500)
    elif not wg_exist and bird_exist:
        return web.Response(body="bird only", status=500)

    wg_regex = (
        r"\[Interface\]\n"
        r"ListenPort = (?P<port>[0-9]+)\n"
        r"Table = off\n"
        r"(?:MTU = (?P<mtu>[0-9]+)\n)?"
        r"PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n"
        r"PostUp = ip addr add (?P<my_lla>fe80::[0-9a-f:]+)/64(?: peer (?P<peer_lla>fe80::[0-9a-f:]+)/64)? dev %i\n"
        r"PostUp = ip addr add (?P<my_ula>f[cd][0-9a-f:]+)/128(?: peer (?P<peer_ula>f[cd][0-9a-f:]+)/128)? dev %i\n"
        r"PostUp = ip addr add " + str(base.MY_DN42_IPv4_ADDRESS) + r"/32(?: peer (?P<peer_ipv4>[0-9.]+)/32)? dev %i\n"
        r"\[Peer\]\n"
        r"PublicKey = (?P<pubkey>.{43}=)\n"
        r"(?:PresharedKey = (?P<psk>.{43}=)\n)?"
        r"(?:Endpoint = (?P<clearnet>.+:[0-9]{,5})\n)?"
        r"AllowedIPs = "
    )
    bird_regex_v4 = r"protocol bgp DN42_" + str(asn) + r"_v4 from dn42_peers \{\n" r"(?:(?: +.*?\n)*?.*\n)+?" r"\}"
    bird_regex_v4_only = (
        r" {4}ipv6 \{\n"
        r"(?: {4,}.*?\n)*?"
        r"(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n"
        r"(?: {4,}.*?\n)*?"
        r" {4}\};"
    )
    bird_regex_v6 = r"protocol bgp DN42_" + str(asn) + r"_v6 from dn42_peers \{\n" r"(?:(?: +.*?\n)*?.*\n)+?" r"\}"
    bird_regex_v6_only = (
        r" {4}ipv4 \{\n"
        r"(?: {4,}.*?\n)*?"
        r"(?:(?: {8}import none;\n(?: {4,}.*?\n)*? {8}export none;)|(?: {8}export none;\n(?: {4,}.*?\n)*? {8}import none;))\n"
        r"(?: {4,}.*?\n)*?"
        r" {4}\};"
    )
    bird_regex_decs = r'^ {4}description "(.*)";$'

    with open(f"/etc/wireguard/dn42-{asn}.conf", "r") as f:
        wg_raw = f.read()
    with open(f"/etc/bird/dn42_peers/{asn}.conf", "r") as f:
        bird_raw = f.read()
    try:
        wg_match = re.search(wg_regex, wg_raw, re.MULTILINE)
        wg_groups = wg_match.groupdict()
    except BaseException:
        return web.Response(body="wg error", status=500)
    mtu = int(wg_groups["mtu"]) if wg_groups["mtu"] else base.DEFAULT_MTU
    if wg_groups["peer_lla"]:
        v6 = wg_groups["peer_lla"]
        my_v6 = wg_groups["my_lla"]
    elif wg_groups["peer_ula"]:
        v6 = wg_groups["peer_ula"]
        my_v6 = wg_groups["my_ula"]
    else:
        v6 = None
        my_v6 = wg_groups["my_ula"]
    my_v4 = str(base.MY_DN42_IPv4_ADDRESS) if wg_groups["peer_ipv4"] else None
    psk = wg_groups["psk"] if wg_groups["psk"] else None
    clearnet = wg_groups["clearnet"] if wg_groups["clearnet"] else None

    desc = "N.A."
    session = ""
    session_name = []
    if matches := re.findall(bird_regex_v6, bird_raw, re.MULTILINE):
        session_name.append(f"DN42_{asn}_v6")
        if re.findall(bird_regex_v6_only, matches[0], re.MULTILINE):
            session = "IPv6 Session with IPv6 channel only"
        else:
            session = "IPv6 Session with IPv6 & IPv4 Channels"
        try:
            desc = re.search(bird_regex_decs, matches[0], re.MULTILINE).group(1)
        except BaseException:
            pass
    if matches := re.findall(bird_regex_v4, bird_raw, re.MULTILINE):
        session_name.append(f"DN42_{asn}_v4")
        if re.findall(bird_regex_v4_only, matches[0], re.MULTILINE):
            if session == "":
                session += "IPv4 Session with IPv4 channel only"
            elif session == "IPv6 Session with IPv6 channel only":
                session = "IPv6 & IPv4 Session with their own channels"
            else:
                return web.Response(body="session error", status=500)
        else:
            session = "IPv4 Session with IPv6 & IPv4 Channels"
        try:
            desc = re.search(bird_regex_decs, matches[0], re.MULTILINE).group(1)
        except BaseException:
            pass
    if not session_name:
        return web.Response(body="no session", status=500)

    out = simple_run(f"wg show dn42-{asn} latest-handshakes")
    if out:
        if out == "Unable to access interface: No such device":
            wg_last_handshake = 0
        else:
            parts = out.split()
            # parts[0] should be peer pubkey, but to be robust we only
            # check length and parse the timestamp if present.
            if len(parts) >= 2:
                try:
                    wg_last_handshake = int(parts[1])
                except ValueError:
                    wg_last_handshake = 0
            else:
                wg_last_handshake = 0
    else:
        wg_last_handshake = 0

    out = simple_run(f"wg show dn42-{asn} transfer")
    if out:
        if out == "Unable to access interface: No such device":
            wg_transfer = [0, 0]
        else:
            parts = out.split()
            if len(parts) >= 3:
                try:
                    wg_transfer = [int(parts[1]), int(parts[2])]
                except ValueError:
                    wg_transfer = [0, 0]
            else:
                wg_transfer = [0, 0]
    else:
        wg_transfer = [0, 0]
    bird_status = {}
    for the_session in session_name:
        out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show protocols {the_session}").splitlines()
        if len(out) != 3:
            return web.Response(body="bird error", status=500)
        out = out[2].strip().split(maxsplit=6)
        if out[0] != the_session:
            return web.Response(body="bird error", status=500)
        bird_status[the_session] = [out[5] if len(out) >= 6 else "N/A", "", {}]
        try:
            bird_status[the_session][1] = out[6]
        except IndexError:
            pass
        if len(out) >= 6 and out[5] == "Established":
            out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show protocols all {the_session}")
            out = [i.strip().splitlines() for i in out.split("Channel ")]
            out = {
                i[0].strip(): {j.split(":", 1)[0].strip(): j.split(":", 1)[1].strip() for j in i[1:]}
                for i in out
                if i[0].strip().startswith("ipv")
            }
            for k, v in out.items():
                if v["State"] == "UP" and v["Output filter"] == "(unnamed)":
                    bird_status[the_session][2][k[3]] = v["Routes"]

    return web.json_response(
        {
            "port": wg_groups["port"],
            "mtu": mtu,
            "v6": v6,
            "v4": wg_groups["peer_ipv4"],
            "clearnet": clearnet,
            "pubkey": wg_groups["pubkey"],
            "psk": psk,
            "desc": desc,
            "session": session,
            "session_name": session_name,
            "my_v6": my_v6,
            "my_v4": my_v4,
            "my_pubkey": base.MY_WG_PUBLIC_KEY,
            "wg_last_handshake": wg_last_handshake,
            "wg_transfer": wg_transfer,
            "bird_status": bird_status,
            "net_support": base.NET_SUPPORT,
            "lla": str(base.MY_DN42_LINK_LOCAL_ADDRESS),
        }
    )


@base.routes.post("/peer")
@set_sentry
async def setup_peer(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret == base.SECRET:
        try:
            peer_info = await request.json()
        except BaseException:
            return web.Response(status=400)
    else:
        return web.Response(status=403)

    current_peer_num = get_current_peer_num()
    if current_peer_num is None:
        return web.Response(body="wireguard and bird config not match", status=500)
    if not (
        os.path.exists(f"/etc/wireguard/dn42-{peer_info['ASN']}.conf")
        and os.path.exists(f"/etc/bird/dn42_peers/{peer_info['ASN']}.conf")
    ) and ((base.MAX_PEERS != 0 and current_peer_num >= base.MAX_PEERS) or not base.OPEN):
        return web.Response(status=503)

    ula = None
    ll = None
    ipv4 = None
    try:
        if ip_address(peer_info["IPv6"]) in IPv6Network("fc00::/7"):
            ula = str(ip_address(peer_info["IPv6"]))
    except BaseException:
        pass
    try:
        if ip_address(peer_info["IPv6"]) in IPv6Network("fe80::/64"):
            ll = str(ip_address(peer_info["IPv6"]))
    except BaseException:
        pass
    try:
        if any(
            ip_address(peer_info["IPv4"]) in n for n in [IPv4Network("172.20.0.0/14"), IPv4Network("10.127.0.0/16")]
        ):
            ipv4 = str(ip_address(peer_info["IPv4"]))
    except BaseException:
        pass
    try:
        my_lla = str(ip_address(peer_info["Request-LinkLocal"]))
    except BaseException:
        my_lla = str(base.MY_DN42_LINK_LOCAL_ADDRESS)
    wg = (
        "# {comment}\n"
        "[Interface]\n"
        "ListenPort = {port}\n"
        "Table = off\n"
        "MTU = {mtu}\n"
        "PostUp = wg set %i private-key /etc/wireguard/dn42-privatekey\n"
        "PostUp = ip addr add {my_lla}/64{ll} dev %i\n"
        "PostUp = ip addr add {my_ula}/128{ula} dev %i\n"
        "PostUp = ip addr add {my_ipv4}/32{ipv4} dev %i\n"
        "[Peer]\n"
        "PublicKey = {pubkey}\n"
        "{psk}"
        "Endpoint = {clearnet}\n"
        "AllowedIPs = 172.20.0.0/14, 10.0.0.0/8, 172.31.0.0/16, fd00::/8, fe80::/64\n"
    )
    psk_line = f"PresharedKey = {peer_info['PresharedKey']}\n" if peer_info.get("PresharedKey") else ""
    final_wg_text = wg.format(
        comment=f"{peer_info['ASN']} - {peer_info['Contact']}",
        mtu=peer_info.get("MTU", base.DEFAULT_MTU),
        port=peer_info["Port"],
        ll=(f" peer {ll}/64" if ll else ""),
        ula=(f" peer {ula}/128" if ula else ""),
        ipv4=(f" peer {ipv4}/32" if ipv4 else ""),
        my_lla=my_lla,
        my_ula=str(base.MY_DN42_ULA_ADDRESS),
        my_ipv4=str(base.MY_DN42_IPv4_ADDRESS),
        pubkey=peer_info["PublicKey"],
        psk=psk_line,
        clearnet=peer_info["Clearnet"],
    )
    if peer_info["Clearnet"] is None:
        final_wg_text = final_wg_text.replace("Endpoint = None\n", "")
    with open(f"/etc/wireguard/dn42-{peer_info['ASN']}.conf", "w") as f:
        f.write(final_wg_text)

    def gen_bird_protocol(version, only):
        text = (
            f"protocol bgp DN42_{peer_info['ASN']}_v{version} from dn42_peers "
            "{\n"
            f"    neighbor {peer_info[f'IPv{version}']} % 'dn42-{peer_info['ASN']}' external;\n"
            f'    description "{peer_info["Contact"]}";\n'
        )
        if only is True:
            if version == 6:
                text += "    ipv4 {\n"
            elif version == 4:
                text += "    ipv6 {\n"
            text += "        import none;\n" "        export none;\n" "    };\n"
        text += "}\n"
        return text

    if peer_info["Channel"] == "IPv6 only":
        bird = gen_bird_protocol(6, True)
    elif peer_info["Channel"] == "IPv4 only":
        bird = gen_bird_protocol(4, True)
    elif peer_info["Channel"] == "IPv6 & IPv4":
        if peer_info["MP-BGP"] == "IPv6":
            bird = gen_bird_protocol(6, False)
        elif peer_info["MP-BGP"] == "IPv4":
            bird = gen_bird_protocol(4, False)
        elif peer_info["MP-BGP"] == "Not supported":
            bird = gen_bird_protocol(6, True) + "\n" + gen_bird_protocol(4, True)
    with open(f"/etc/bird/dn42_peers/{peer_info['ASN']}.conf", "w") as f:
        f.write(bird)

    # In container: directly bring interface up; do not touch systemd units
    # 仅执行一次 wg-quick up；按输出包含 "ip link delete dev" 判定错误
    try:
        out_wg = simple_run(f"wg-quick up dn42-{peer_info['ASN']}", timeout=10)
    except Exception:
        out_wg = "wg-quick timeout or error"
    wg_ok = ("ip link delete dev" not in out_wg.lower())
    # 即便三次都失败，这里也不抛异常，由上层逻辑或人工排查
    simple_run(f"birdc -s {base.BIRD_CTL_PATH} c")
    if base.VNSTAT_AUTO_ADD:
        simple_run(f'vnstat --add -i dn42-{peer_info["ASN"]}')

    return web.Response(status=200)


@base.routes.post("/remove")
@set_sentry
async def remove_peer(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret == base.SECRET:
        try:
            asn = int(await request.text())
        except BaseException:
            return web.Response(status=400)
    else:
        return web.Response(status=403)

    # In container: directly bring interface down; do not touch systemd units
    simple_run(f"wg-quick down dn42-{asn}")
    try:
        os.remove(f"/etc/wireguard/dn42-{asn}.conf")
    except BaseException:
        pass
    try:
        os.remove(f"/etc/bird/dn42_peers/{asn}.conf")
    except BaseException:
        pass
    simple_run(f"birdc -s {base.BIRD_CTL_PATH} c")
    if base.VNSTAT_AUTO_REMOVE:
        simple_run(f"vnstat --remove -i dn42-{asn} --force")

    return web.Response(status=200)


@base.routes.post("/restart")
@set_sentry
async def restart_peer(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret == base.SECRET:
        try:
            asn = int(await request.text())
        except BaseException:
            return web.Response(status=400)
    else:
        return web.Response(status=403)

    # In container: restart by down+up via wg-quick directly
    try:
        simple_run(f"wg-quick down dn42-{asn}", timeout=10)
    except Exception:
        # 如果 down 失败（例如接口不存在），继续尝试 up
        pass
    # 仅执行一次 wg-quick up；按输出包含 "ip link delete dev" 判定错误
    try:
        out_wg = simple_run(f"wg-quick up dn42-{asn}", timeout=10)
    except Exception:
        out_wg = "wg-quick timeout or error"
    out_v4 = simple_run(f"birdc -s {base.BIRD_CTL_PATH} restart DN42_{asn}_v4")
    out_v6 = simple_run(f"birdc -s {base.BIRD_CTL_PATH} restart DN42_{asn}_v6")
    wg_error = ("ip link delete dev" in out_wg.lower())
    if "syntax error" in out_v4 and "syntax error" in out_v6:
        if wg_error:
            return web.Response(status=404)
        else:
            return web.Response(body="bird error", status=500)
    else:
        if wg_error:
            return web.Response(body="wg error", status=500)
        else:
            return web.Response(status=200)


@base.routes.post("/errorlist")
@set_sentry
async def errorlist(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)

    wg_dir = "/etc/wireguard"
    bird_dir = "/etc/bird/dn42_peers"

    wg_asns = set()
    bird_asns = set()
    try:
        for f in os.listdir(wg_dir):
            if f.startswith("dn42-") and f.endswith(".conf") and f[5:-5].isdigit():
                wg_asns.add(int(f[5:-5]))
    except OSError:
        pass
    try:
        for f in os.listdir(bird_dir):
            if f.endswith(".conf") and f[:-5].isdigit():
                bird_asns.add(int(f[:-5]))
    except OSError:
        pass

    all_asns = sorted(wg_asns | bird_asns)
    now = int(time.time())
    errors = []

    for asn in all_asns:
        issues = []
        # Config mismatch
        if asn in wg_asns and asn not in bird_asns:
            issues.append("WireGuard config exists but BIRD config missing")
        elif asn not in wg_asns and asn in bird_asns:
            issues.append("BIRD config exists but WireGuard config missing")

        # WireGuard checks
        if asn in wg_asns:
            try:
                out = simple_run(f"wg show dn42-{asn} latest-handshakes")
                if not out or out == "Unable to access interface: No such device":
                    issues.append("WireGuard interface down or not accessible")
                else:
                    parts = out.split()
                    if len(parts) >= 2:
                        try:
                            handshake_ts = int(parts[1])
                            if handshake_ts == 0:
                                issues.append("WireGuard never handshaked")
                            elif now - handshake_ts > WG_HANDSHAKE_STALE_SECONDS:
                                issues.append("WireGuard handshake stale")
                        except ValueError:
                            issues.append("WireGuard handshake data unreadable")
                    else:
                        issues.append("WireGuard handshake data empty")
            except Exception:
                issues.append("WireGuard status check failed")

        # BIRD BGP checks — only for sessions that exist in config
        if asn in bird_asns:
            try:
                with open(f"/etc/bird/dn42_peers/{asn}.conf", "r") as f:
                    bird_raw = f.read()
                sessions_to_check = []
                if re.search(rf"protocol bgp DN42_{asn}_v4 ", bird_raw):
                    sessions_to_check.append("v4")
                if re.search(rf"protocol bgp DN42_{asn}_v6 ", bird_raw):
                    sessions_to_check.append("v6")
            except OSError:
                sessions_to_check = []
                issues.append("BIRD config unreadable")
            for suffix in sessions_to_check:
                session = f"DN42_{asn}_{suffix}"
                try:
                    out = simple_run(f"birdc -s {base.BIRD_CTL_PATH} show protocols {session}")
                    lines = out.splitlines()
                    if len(lines) != 3:
                        issues.append(f"BIRD {session} not found or error")
                        continue
                    fields = lines[2].strip().split(maxsplit=6)
                    if len(fields) < 6 or fields[0] != session:
                        issues.append(f"BIRD {session} parse error")
                        continue
                    state = fields[5]
                    if state != "Established":
                        issues.append(f"BIRD {session} state: {state}")
                except Exception:
                    issues.append(f"BIRD {session} check failed")

        if issues:
            errors.append({"asn": asn, "issues": issues})

    return web.json_response(errors)
