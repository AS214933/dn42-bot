import ipaddress
import json
from datetime import datetime, timezone

import base
import config
import tools
from base import bot, db_privilege
from telebot.types import (
    InputFile,
    InlineKeyboardButton,
    InlineKeyboardMarkup,
    ReplyKeyboardRemove,
)
from tools.peer_registry import add_peer, load_registry


def _get_v2_nodes():
    v2 = tools.get_v2_nodes()
    return sorted(v2 & base.servers.keys())


def _node_markup(prefix, nodes, selected=""):
    markup = InlineKeyboardMarkup()
    for node in nodes:
        check = "\u2705 " if node == selected else ""
        markup.row(
            InlineKeyboardButton(
                text=f"{check}{base.servers[node]}",
                callback_data=f"{prefix}_{node}",
            )
        )
    return markup


def _get_peer_info(node, asn):
    resp = tools.get_from_agent("info", str(asn), server=[node])
    result = resp.get(node)
    if not result or result.status != 200:
        return None
    try:
        return json.loads(result.text)
    except Exception:
        return None


def _build_peer_export_entry(node, asn, peer_info):
    session_name = peer_info.get("session_name", [])
    if isinstance(session_name, list) and len(session_name) >= 1:
        # Strip _v4/_v6 suffix to get the base WireGuard interface name
        base_name = session_name[0]
        if base_name.endswith(("_v4", "_v6")):
            base_name = base_name.rsplit("_", 1)[0]
        wg_interface_name = base_name
        bgp_proto_name = base_name
        bird_config_filename = f"{base_name}.conf"
    else:
        wg_interface_name = ""
        bgp_proto_name = ""
        bird_config_filename = ""

    psk = peer_info.get("psk")
    if psk is not None and psk == "":
        psk = None

    return {
        "node_id": node,
        "node_name": base.servers.get(node, node),
        "remote_asn": asn,
        "remote_pubkey": peer_info.get("pubkey", ""),
        "remote_endpoint": peer_info.get("clearnet", ""),
        "remote_lla": peer_info.get("v6", ""),
        "contact_email": peer_info.get("desc", ""),
        "wg_listen_port": peer_info.get("port", 0),
        "wg_interface_name": wg_interface_name,
        "bgp_proto_name": bgp_proto_name,
        "bird_config_filename": bird_config_filename,
        "wg_managed": True,
        "mtu": peer_info.get("mtu"),
        "wg_preshared_key": psk,
        "status": "active",
    }


def _build_export_json(peers):
    return {
        "version": 1,
        "exported_at": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
        "peers": peers,
    }


def _send_export_file(chat_id, data, filename="peers_export.json"):
    payload = json.dumps(data, indent=2, ensure_ascii=False)
    bio = __import__("io").BytesIO(payload.encode("utf-8"))
    bio.name = filename
    bot.send_document(
        chat_id,
        InputFile(bio),
        caption=f"Exported {len(data.get('peers', []))} peer(s)\n导出了 {len(data.get('peers', []))} 个 Peer",
    )


@bot.message_handler(commands=["listpeers"], is_private_chat=True)
def listpeers_command(message):
    chat_id = message.chat.id

    if chat_id not in db_privilege:
        bot.send_message(
            chat_id,
            "This command is only available to privileged users.\n此命令仅限特权用户使用。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    v2_nodes = _get_v2_nodes()
    if not v2_nodes:
        bot.send_message(
            chat_id,
            "No v2 agent nodes are currently online.\n当前没有在线的 v2 Agent 节点。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    bot.send_message(
        chat_id,
        "Select a node to list peers:\n选择一个节点以列出 Peer：",
        reply_markup=_node_markup("listpeers", v2_nodes),
    )


@bot.callback_query_handler(func=lambda call: call.data.startswith("listpeers_") and
                             not call.data.startswith("listpeers_peerlist_") and
                             not call.data.startswith("listpeers_export_") and
                             not call.data.startswith("listpeers_exportasn_") and
                             not call.data.startswith("listpeers_import_"))
def listpeers_node_callback(call):
    node = call.data.split("_", 1)[1]
    chat_id = call.message.chat.id

    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    if node not in base.servers:
        bot.answer_callback_query(call.id, "Node offline.", show_alert=True)
        return

    resp = tools.get_from_agent("listpeers", "{}", server=[node])
    result = resp.get(node)
    if not result or result.status != 200:
        bot.answer_callback_query(call.id, "Agent unreachable.", show_alert=True)
        return

    try:
        asns = json.loads(result.text).get("asns", [])
    except Exception:
        bot.answer_callback_query(call.id, "Invalid agent response.", show_alert=True)
        return

    node_display = base.servers[node]
    if not asns:
        text = f"*{node_display}*\n\nNo peers found.\n未找到 Peer。"
    else:
        lines = [f"*{node_display}*\n", f"Peers ({len(asns)}):\n"]
        for asn in asns:
            lines.append(f"  \u2022 `AS{asn}`")
        text = "\n".join(lines)

    markup = InlineKeyboardMarkup()
    markup.row(
        InlineKeyboardButton(
            text="\U0001f4e5 Import | \u5bfc\u5165",
            callback_data=f"listpeers_import_{node}",
        ),
        InlineKeyboardButton(
            text="\U0001f4e4 Export | \u5bfc\u51fa",
            callback_data=f"listpeers_export_{node}",
        ),
    )
    markup.row(
        InlineKeyboardButton(
            text="\u2b05\ufe0f Back | \u8fd4\u56de",
            callback_data="listpeers",
        )
    )

    try:
        bot.edit_message_text(
            text,
            parse_mode="Markdown",
            chat_id=chat_id,
            message_id=call.message.message_id,
            reply_markup=markup,
        )
    except Exception:
        pass


@bot.callback_query_handler(func=lambda call: call.data == "listpeers")
def listpeers_back_callback(call):
    chat_id = call.message.chat.id
    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    v2_nodes = _get_v2_nodes()
    if not v2_nodes:
        bot.answer_callback_query(call.id, "No v2 nodes online.", show_alert=True)
        return

    try:
        bot.edit_message_text(
            "Select a node to list peers:\n选择一个节点以列出 Peer：",
            chat_id=chat_id,
            message_id=call.message.message_id,
            reply_markup=_node_markup("listpeers", v2_nodes),
        )
    except Exception:
        pass


@bot.callback_query_handler(func=lambda call: call.data.startswith("listpeers_export_") and
                             not call.data.startswith("listpeers_exportall_") and
                             not call.data.startswith("listpeers_exportasn_"))
def listpeers_export_callback(call):
    node = call.data.split("_", 2)[2]
    chat_id = call.message.chat.id

    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    markup = InlineKeyboardMarkup()
    markup.row(
        InlineKeyboardButton(
            text="\U0001f3af Specify AS | \u6307\u5b9a AS",
            callback_data=f"listpeers_exportasn_{node}",
        )
    )
    markup.row(
        InlineKeyboardButton(
            text="\U0001f4e6 Export All | \u5168\u90e8\u5bfc\u51fa",
            callback_data=f"listpeers_exportall_{node}",
        )
    )
    markup.row(
        InlineKeyboardButton(
            text="\u2b05\ufe0f Back | \u8fd4\u56de",
            callback_data=f"listpeers_{node}",
        )
    )

    try:
        bot.edit_message_text(
            f"Export peers from *{base.servers.get(node, node)}*:\n"
            f"\u4ece *{base.servers.get(node, node)}* \u5bfc\u51fa Peer\uff1a",
            parse_mode="Markdown",
            chat_id=chat_id,
            message_id=call.message.message_id,
            reply_markup=markup,
        )
    except Exception:
        pass


@bot.callback_query_handler(func=lambda call: call.data.startswith("listpeers_exportall_"))
def listpeers_exportall_callback(call):
    node = call.data.split("_", 2)[2]
    chat_id = call.message.chat.id

    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    bot.answer_callback_query(call.id, "Exporting...")

    resp = tools.get_from_agent("listpeers", "{}", server=[node])
    result = resp.get(node)
    if not result or result.status != 200:
        bot.send_message(chat_id, "Agent unreachable.\nAgent 不可达。")
        return

    try:
        asns = json.loads(result.text).get("asns", [])
    except Exception:
        bot.send_message(chat_id, "Invalid agent response.\nAgent 响应异常。")
        return

    if not asns:
        bot.send_message(chat_id, "No peers to export.\n没有可导出的 Peer。")
        return

    peer_entries = []
    errors = []
    for asn in asns:
        peer_info = _get_peer_info(node, asn)
        if peer_info and isinstance(peer_info, dict):
            peer_entries.append(_build_peer_export_entry(node, asn, peer_info))
        else:
            errors.append(asn)

    if not peer_entries:
        bot.send_message(chat_id, "Failed to fetch any peer details.\n获取 Peer 详情失败。")
        return

    export_data = _build_export_json(peer_entries)
    _send_export_file(chat_id, export_data)

    if errors:
        err_list = ", ".join(f"AS{a}" for a in errors)
        bot.send_message(
            chat_id,
            f"Warning: failed to fetch details for: {err_list}\n"
            f"\u8b66\u544a\uff1a\u4ee5\u4e0b ASN \u83b7\u53d6\u8be6\u60c5\u5931\u8d25\uff1a{err_list}",
        )


@bot.callback_query_handler(func=lambda call: call.data.startswith("listpeers_exportasn_"))
def listpeers_exportasn_callback(call):
    node = call.data.split("_", 2)[2]
    chat_id = call.message.chat.id

    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    bot.answer_callback_query(call.id)

    msg = bot.send_message(
        chat_id,
        "Please enter the AS number(s) to export, separated by spaces or commas:\n"
        "\u8bf7\u8f93\u5165\u8981\u5bfc\u51fa\u7684 AS \u53f7\uff0c\u7528\u7a7a\u683c\u6216\u9017\u53f7\u5206\u9694\uff1a\n\n"
        "Abbreviation supported (e.g. `1234` \u2192 `4242421234`)\n"
        "\u652f\u6301\u7f29\u5199\uff08\u4f8b\u5982 `1234` \u2192 `4242421234`\uff09",
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
    bot.register_next_step_handler(msg, _handle_export_asn_input, node)


def _handle_export_asn_input(message, node):
    chat_id = message.chat.id

    if message.text and message.text.strip().lower() == "/cancel":
        bot.send_message(
            chat_id,
            "Current operation has been cancelled.\n\u5f53\u524d\u64cd\u4f5c\u5df2\u88ab\u53d6\u6d88\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    if chat_id not in db_privilege:
        return

    raw_text = message.text or ""
    tokens = [t.strip() for t in raw_text.replace(",", " ").split() if t.strip()]
    if not tokens:
        bot.send_message(
            chat_id,
            "No AS numbers provided. Operation cancelled.\n\u672a\u63d0\u4f9b AS \u53f7\uff0c\u64cd\u4f5c\u53d6\u6d88\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    asns = []
    invalid = []
    for token in tokens:
        parsed = tools.extract_asn(token, privilege=True)
        if parsed is not None:
            asns.append(parsed)
        else:
            invalid.append(token)

    if invalid:
        bot.send_message(
            chat_id,
            f"Invalid AS number(s): {', '.join(invalid)}\n"
            f"\u65e0\u6548\u7684 AS \u53f7\uff1a{', '.join(invalid)}",
            reply_markup=ReplyKeyboardRemove(),
        )
        if not asns:
            return

    resp = tools.get_from_agent("listpeers", "{}", server=[node])
    result = resp.get(node)
    if not result or result.status != 200:
        bot.send_message(chat_id, "Agent unreachable.\nAgent \u4e0d\u53ef\u8fbe\u3002")
        return

    try:
        agent_asns = set(json.loads(result.text).get("asns", []))
    except Exception:
        bot.send_message(chat_id, "Invalid agent response.\nAgent \u54cd\u5e94\u5f02\u5e38\u3002")
        return

    valid_asns = [a for a in asns if a in agent_asns]
    not_found = [a for a in asns if a not in agent_asns]

    if not_found:
        nf_list = ", ".join(f"AS{a}" for a in not_found)
        bot.send_message(
            chat_id,
            f"The following ASN(s) were not found on this node:\n"
            f"\u4ee5\u4e0b ASN \u5728\u8be5\u8282\u70b9\u4e0a\u672a\u627e\u5230\uff1a\n{nf_list}",
        )

    if not valid_asns:
        bot.send_message(chat_id, "No valid peers to export.\n\u6ca1\u6709\u53ef\u5bfc\u51fa\u7684\u6709\u6548 Peer\u3002")
        return

    peer_entries = []
    errors = []
    for asn in valid_asns:
        peer_info = _get_peer_info(node, asn)
        if peer_info and isinstance(peer_info, dict):
            peer_entries.append(_build_peer_export_entry(node, asn, peer_info))
        else:
            errors.append(asn)

    if not peer_entries:
        bot.send_message(chat_id, "Failed to fetch any peer details.\n\u83b7\u53d6 Peer \u8be6\u60c5\u5931\u8d25\u3002")
        return

    export_data = _build_export_json(peer_entries)
    _send_export_file(chat_id, export_data)

    if errors:
        err_list = ", ".join(f"AS{a}" for a in errors)
        bot.send_message(
            chat_id,
            f"Warning: failed to fetch details for: {err_list}\n"
            f"\u8b66\u544a\uff1a\u4ee5\u4e0b ASN \u83b7\u53d6\u8be6\u60c5\u5931\u8d25\uff1a{err_list}",
        )


def _validate_peer_entry(entry):
    """Returns None if valid, or an error reason string."""
    required = [
        "node_id", "remote_asn", "remote_pubkey", "remote_endpoint",
        "remote_lla", "wg_listen_port", "wg_interface_name", "wg_managed",
    ]
    for field in required:
        if field not in entry or entry[field] is None:
            return f"missing {field}"

    node_id = entry["node_id"]
    if node_id not in config.SERVERS:
        return "node not found"

    try:
        int(entry["remote_asn"])
    except (TypeError, ValueError):
        return "invalid remote_asn"

    pubkey = str(entry["remote_pubkey"])
    if len(pubkey) < 40:
        return "remote_pubkey too short"

    lla = str(entry["remote_lla"])
    try:
        if ipaddress.ip_address(lla) not in ipaddress.ip_network("fe80::/10"):
            return "remote_lla not in fe80::/10"
    except ValueError:
        return "invalid remote_lla"

    mtu = entry.get("mtu")
    if mtu is not None and mtu != "":
        try:
            mtu_int = int(mtu)
            if not (576 <= mtu_int <= 9000):
                return "mtu out of range (576-9000)"
        except (TypeError, ValueError):
            return "invalid mtu"

    return None


def _parse_import_json(raw_bytes):
    """Returns (peers_list, error_string). error_string is None on success."""
    try:
        data = json.loads(raw_bytes)
    except (json.JSONDecodeError, UnicodeDecodeError) as e:
        return None, f"Invalid JSON: {e}"

    if isinstance(data, list):
        return data, None
    if isinstance(data, dict):
        peers = data.get("peers")
        if isinstance(peers, list):
            return peers, None
        return None, "Unrecognized format: missing 'peers' array."
    return None, "Unrecognized format: expected JSON object or array."


@bot.callback_query_handler(func=lambda call: call.data.startswith("listpeers_import_"))
def listpeers_import_callback(call):
    node = call.data.split("_", 2)[2]
    chat_id = call.message.chat.id

    if chat_id not in db_privilege:
        bot.answer_callback_query(call.id, "No permission.", show_alert=True)
        return

    bot.answer_callback_query(call.id)

    msg = bot.send_message(
        chat_id,
        "Please upload a JSON file to import peers:\n"
        "\u8bf7\u4e0a\u4f20 JSON \u6587\u4ef6\u4ee5\u5bfc\u5165 Peer\uff1a\n\n"
        "Supported formats:\n\u652f\u6301\u7684\u683c\u5f0f\uff1a\n"
        "  \u2022 Full wrapper: `{\"version\":1, \"peers\":[...]}`\n"
        "  \u2022 Bare array: `[{...}, {...}]`\n\n"
        "Type /cancel to abort.\n\u8f93\u5165 /cancel \u53d6\u6d88\u3002",
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
    bot.register_next_step_handler(msg, _handle_import_file, node)


def _handle_import_file(message, node):
    chat_id = message.chat.id

    if message.text and message.text.strip().lower() == "/cancel":
        bot.send_message(
            chat_id,
            "Current operation has been cancelled.\n\u5f53\u524d\u64cd\u4f5c\u5df2\u88ab\u53d6\u6d88\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    if chat_id not in db_privilege:
        return

    if not message.document:
        bot.send_message(
            chat_id,
            "No file received. Please upload a JSON file.\n"
            "\u672a\u6536\u5230\u6587\u4ef6\u3002\u8bf7\u4e0a\u4f20 JSON \u6587\u4ef6\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    file_name = message.document.file_name or ""
    if not file_name.lower().endswith(".json"):
        bot.send_message(
            chat_id,
            "Only .json files are accepted.\n\u4ec5\u63a5\u53d7 .json \u6587\u4ef6\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    try:
        file_info = bot.get_file(message.document.file_id)
        raw_bytes = bot.download_file(file_info.file_path)
    except Exception:
        bot.send_message(
            chat_id,
            "Failed to download the file.\n\u4e0b\u8f7d\u6587\u4ef6\u5931\u8d25\u3002",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    peers_list, parse_err = _parse_import_json(raw_bytes)
    if parse_err:
        bot.send_message(
            chat_id,
            f"Failed to parse file:\n{parse_err}\n"
            f"\u89e3\u6790\u6587\u4ef6\u5931\u8d25\uff1a\n{parse_err}",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    registry = load_registry()
    imported = 0
    skipped = 0
    errors = []

    for idx, entry in enumerate(peers_list):
        if not isinstance(entry, dict):
            errors.append({"index": idx, "reason": "not a JSON object"})
            continue

        err = _validate_peer_entry(entry)
        if err:
            node_id = entry.get("node_id", "?")
            asn = entry.get("remote_asn", "?")
            errors.append({"node_id": node_id, "asn": asn, "reason": err})
            continue

        node_id = entry["node_id"]
        remote_asn = int(entry["remote_asn"])
        key = (node_id, remote_asn)

        if key in registry:
            skipped += 1
            continue

        info = {k: v for k, v in entry.items() if k not in ("node_id", "remote_asn")}
        if not info.get("status"):
            info["status"] = "active"

        add_peer(node_id, remote_asn, info)
        imported += 1

    total = len(peers_list)
    result_lines = [
        f"Import completed.\n\u5bfc\u5165\u5b8c\u6210\u3002",
        f"",
        f"  Total | \u603b\u8ba1: {total}",
        f"  Imported | \u5bfc\u5165: {imported}",
        f"  Skipped (duplicate) | \u8df3\u8fc7\uff08\u91cd\u590d\uff09: {skipped}",
        f"  Errors | \u9519\u8bef: {len(errors)}",
    ]

    if errors:
        result_lines.append("")
        result_lines.append("Error details | \u9519\u8bef\u8be6\u60c5\uff1a")
        for err in errors[:20]:
            nid = err.get("node_id", err.get("index", "?"))
            asn = err.get("asn", "")
            reason = err.get("reason", "unknown")
            if asn:
                result_lines.append(f"  \u2022 {nid} / AS{asn}: {reason}")
            else:
                result_lines.append(f"  \u2022 [{nid}]: {reason}")
        if len(errors) > 20:
            result_lines.append(f"  ... and {len(errors) - 20} more")

    bot.send_message(
        chat_id,
        "\n".join(result_lines),
        reply_markup=ReplyKeyboardRemove(),
    )
