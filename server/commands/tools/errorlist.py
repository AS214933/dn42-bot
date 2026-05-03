import json

import base
import config
import tools
from base import bot, db_privilege
from telebot.types import ReplyKeyboardRemove


@bot.message_handler(commands=["errorlist"], is_private_chat=True)
def cmd_errorlist(message):
    if message.chat.id not in db_privilege:
        bot.send_message(
            message.chat.id,
            "This command is for admin only.\n此命令仅限管理员使用。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    if not base.servers:
        bot.send_message(
            message.chat.id,
            "No servers online.\n没有在线服务器。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    results = tools.get_from_agent("errorlist", "", timeout=30, retry=2)

    all_errors = {}  # asn -> {region_display: [issues]}
    for region, resp in results.items():
        display = base.servers.get(region, region)
        if resp.status != 200:
            all_errors.setdefault("_server_errors", {})[display] = f"HTTP {resp.status}"
            continue
        try:
            errors = json.loads(resp.text)
        except Exception:
            all_errors.setdefault("_server_errors", {})[display] = "JSON parse error"
            continue
        for entry in errors:
            asn = entry["asn"]
            all_errors.setdefault(asn, {})[display] = entry["issues"]

    if not all_errors and "_server_errors" not in all_errors:
        bot.send_message(
            message.chat.id,
            "No errors found. All peers are healthy.\n未发现错误，所有 Peer 状态正常。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    lines = []
    # Server communication errors
    if "_server_errors" in all_errors:
        lines.append("[Server Errors]")
        for display, err in all_errors["_server_errors"].items():
            lines.append(f"  {display}: {err}")
        lines.append("")

    # Peer errors grouped by ASN
    for asn in sorted(k for k in all_errors if k != "_server_errors"):
        regions = all_errors[asn]
        mnt = tools.get_whoisinfo_by_asn(asn)
        lines.append(f"AS{asn} ({mnt})")
        for display, issues in regions.items():
            for issue in issues:
                lines.append(f"  [{display}] {issue}")

    text = "\n".join(lines)
    msg = f"```\n{text}\n```"

    # Split if too long
    chunks = tools.split_long_msg(msg, limit=4000)
    if chunks is None:
        # Single line too long, send raw
        bot.send_message(message.chat.id, msg, parse_mode="Markdown", reply_markup=ReplyKeyboardRemove())
    elif len(chunks) == 1:
        bot.send_message(message.chat.id, chunks[0], parse_mode="Markdown", reply_markup=ReplyKeyboardRemove())
    else:
        last_msg = message
        for i, chunk in enumerate(chunks):
            if i < len(chunks) - 1:
                last_msg = bot.reply_to(last_msg, chunk, parse_mode="Markdown")
            else:
                bot.reply_to(last_msg, chunk, parse_mode="Markdown", reply_markup=ReplyKeyboardRemove())
