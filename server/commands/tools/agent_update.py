import json

import base
import config
import requests
from base import bot, db_privilege
from telebot.types import InlineKeyboardButton, InlineKeyboardMarkup, ReplyKeyboardRemove


CHANNELS = ("candidate", "stable")


def _get_agent_host(server_key):
    if server_key in config.HOSTS:
        return config.HOSTS[server_key]
    return f"{server_key}.{config.ENDPOINT}"


def _target_label(target):
    if target == "all":
        return "All online nodes"
    return base.servers.get(target, target)


def _target_servers(target):
    if target == "all":
        return sorted(base.servers.keys())
    if target in base.servers:
        return [target]
    return []


def _post_agent_update(server_key, endpoint, payload, timeout):
    host = _get_agent_host(server_key)
    try:
        resp = requests.post(
            f"http://{host}:{config.API_PORT}/{endpoint}",
            data=json.dumps(payload),
            headers={
                "X-DN42-Bot-Api-Secret-Token": config.API_TOKEN,
                "Content-Type": "application/json",
            },
            timeout=timeout,
        )
    except Exception as e:
        return {"ok": False, "error": str(e) or type(e).__name__}

    result = {
        "ok": 200 <= resp.status_code < 300,
        "status": resp.status_code,
    }
    try:
        result["json"] = resp.json()
    except ValueError:
        result["text"] = resp.text[:300]
    return result


def _format_bool(value):
    return "yes" if value else "no"


def _format_update_result(server_key, result):
    title = base.servers.get(server_key, server_key)
    lines = [f"{title}:"]
    if not result.get("ok"):
        if "status" in result:
            detail = result.get("text") or result.get("json") or ""
            lines.append(f"  HTTP {result['status']}: {detail}")
        else:
            lines.append(f"  ERROR: {result.get('error', 'unknown error')}")
        return "\n".join(lines)

    status = result.get("json")
    if not isinstance(status, dict):
        lines.append(f"  HTTP {result.get('status')}: {result.get('text', '')}")
        return "\n".join(lines)

    lines.extend(
        [
            f"  current: {status.get('current_version', 'unknown')}",
            f"  latest: {status.get('latest_version', 'unknown')}",
            f"  channel: {status.get('channel', 'unknown')}",
            f"  update available: {_format_bool(status.get('update_available'))}",
            f"  installed: {_format_bool(status.get('installed'))}",
            f"  restart required: {_format_bool(status.get('restart_required'))}",
        ]
    )
    if status.get("asset_name"):
        lines.append(f"  asset: {status['asset_name']}")
    if status.get("release_url"):
        lines.append(f"  release: {status['release_url']}")
    return "\n".join(lines)


def _run_update(target, action, channel, force=False):
    if channel not in CHANNELS:
        return f"Invalid update channel: {channel}"

    servers = _target_servers(target)
    if not servers:
        return "No online target nodes found."

    endpoint = "update/check" if action == "check" else "update/apply"
    payload = {"channel": channel}
    if action == "apply":
        payload["force"] = force
    timeout = 15 if action == "check" else 60

    header = f"Agent update {action}"
    if force:
        header += " (force)"
    header += f" / {channel} / {_target_label(target)}"
    lines = [header, ""]
    for server_key in servers:
        result = _post_agent_update(server_key, endpoint, payload, timeout)
        lines.append(_format_update_result(server_key, result))
        lines.append("")
    return "\n".join(lines).rstrip()


def _server_keyboard():
    markup = InlineKeyboardMarkup()
    if base.servers:
        markup.row(InlineKeyboardButton(text="All online nodes | 全部在线节点", callback_data="upd:t:all"))
    for key, display in base.servers.items():
        markup.row(InlineKeyboardButton(text=display, callback_data=f"upd:t:{key}"))
    return markup


def _action_keyboard(target):
    markup = InlineKeyboardMarkup()
    for channel in CHANNELS:
        markup.row(
            InlineKeyboardButton(text=f"Check {channel}", callback_data=f"upd:check:{channel}:{target}"),
            InlineKeyboardButton(text=f"Update {channel}", callback_data=f"upd:confirm:{channel}:{target}:0"),
        )
        markup.row(
            InlineKeyboardButton(text=f"Force {channel}", callback_data=f"upd:confirm:{channel}:{target}:1"),
        )
    markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="upd:back"))
    return markup


def _confirm_keyboard(target, channel, force):
    force_value = "1" if force else "0"
    markup = InlineKeyboardMarkup()
    markup.row(
        InlineKeyboardButton(text="Confirm | 确认", callback_data=f"upd:apply:{channel}:{target}:{force_value}"),
    )
    markup.row(
        InlineKeyboardButton(text="Back | 返回", callback_data=f"upd:t:{target}"),
    )
    return markup


def _show_server_list(chat_id, message_id=None):
    text = "Select agent node to update.\n选择要更新的 Agent 节点。"
    markup = _server_keyboard()
    if message_id:
        bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup)
    else:
        bot.send_message(chat_id, text, reply_markup=markup)


def _show_actions(chat_id, target, message_id):
    text = f"Selected: {_target_label(target)}\n选择操作。"
    bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=_action_keyboard(target))


@bot.message_handler(commands=["update"], is_private_chat=True)
def cmd_update(message):
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

    _show_server_list(message.chat.id)


@bot.callback_query_handler(func=lambda call: call.data.startswith("upd:"))
def handle_update_callback(call):
    if call.message.chat.id not in db_privilege:
        bot.answer_callback_query(call.id, "Admin only.", show_alert=True)
        return

    data = call.data.split(":")
    action = data[1] if len(data) > 1 else ""

    if action == "back":
        _show_server_list(call.message.chat.id, call.message.message_id)
        bot.answer_callback_query(call.id)
        return

    if action == "t" and len(data) == 3:
        target = data[2]
        if not _target_servers(target):
            bot.answer_callback_query(call.id, "Target is offline.", show_alert=True)
            return
        _show_actions(call.message.chat.id, target, call.message.message_id)
        bot.answer_callback_query(call.id)
        return

    if action == "confirm" and len(data) == 5:
        channel, target, force_value = data[2], data[3], data[4]
        if not _target_servers(target):
            bot.answer_callback_query(call.id, "Target is offline.", show_alert=True)
            return
        force = force_value == "1"
        text = f"Update {_target_label(target)} using {channel} channel?"
        if force:
            text += "\nForce mode will reinstall even when versions match."
        text += "\n\n确认执行远程更新？"
        bot.edit_message_text(
            text,
            chat_id=call.message.chat.id,
            message_id=call.message.message_id,
            reply_markup=_confirm_keyboard(target, channel, force),
        )
        bot.answer_callback_query(call.id)
        return

    if action in ("check", "apply") and len(data) >= 4:
        channel, target = data[2], data[3]
        force = len(data) >= 5 and data[4] == "1"
        if not _target_servers(target):
            bot.answer_callback_query(call.id, "Target is offline.", show_alert=True)
            return
        bot.answer_callback_query(call.id, "Running...")
        bot.edit_message_text(
            "Running remote update command...\n正在执行远程更新指令...",
            chat_id=call.message.chat.id,
            message_id=call.message.message_id,
        )
        text = _run_update(target, action, channel, force)
        bot.edit_message_text(
            text,
            chat_id=call.message.chat.id,
            message_id=call.message.message_id,
            reply_markup=_action_keyboard(target),
        )
        return

    bot.answer_callback_query(call.id)
