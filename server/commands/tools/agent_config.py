import json
from functools import partial

import base
import config
from base import bot, db_privilege
from telebot.types import InlineKeyboardButton, InlineKeyboardMarkup, ReplyKeyboardRemove

# Fields safe to edit at runtime (subset that admins should manage)
EDITABLE_KEYS = [
    "IGP_PROTOCOL",
    "DEFAULT_MTU",
    "OPEN",
    "MAX_PEERS",
    "MIN_PEER_REQUIREMENT",
    "EXTRA_MSG",
    "VNSTAT_AUTO_ADD",
    "VNSTAT_AUTO_REMOVE",
    "BIRD_CTL_PATH",
    "BIRD_TABLE_4",
    "BIRD_TABLE_6",
    "NET_SUPPORT",
]

# Human-readable descriptions for editable keys
KEY_DESCRIPTIONS = {
    "IGP_PROTOCOL": "IGP Protocol (babel/ospf/empty=auto)",
    "DEFAULT_MTU": "Default MTU",
    "OPEN": "Accept new peers",
    "MAX_PEERS": "Max peers (0=unlimited)",
    "MIN_PEER_REQUIREMENT": "Min peer requirement",
    "EXTRA_MSG": "Extra message",
    "VNSTAT_AUTO_ADD": "Auto vnstat add",
    "VNSTAT_AUTO_REMOVE": "Auto vnstat remove",
    "BIRD_CTL_PATH": "BIRD control socket path",
    "BIRD_TABLE_4": "BIRD IPv4 table name",
    "BIRD_TABLE_6": "BIRD IPv6 table name",
    "NET_SUPPORT": "Network support (JSON)",
}


def _format_config(config_data):
    """Format config dict as readable text."""
    lines = []
    for key in EDITABLE_KEYS:
        desc = KEY_DESCRIPTIONS.get(key, key)
        value = config_data.get(key, "N/A")
        if isinstance(value, bool):
            value = "true" if value else "false"
        elif isinstance(value, dict):
            value = json.dumps(value)
        lines.append(f"  {key}: {value}")
    return "\n".join(lines)


def _get_agent_host(server_key):
    """Resolve agent host for a server key."""
    if server_key in config.HOSTS:
        return config.HOSTS[server_key]
    return f"{server_key}.{config.ENDPOINT}"


def _call_agent_config(server_key, action, data=None):
    """Call agent /config/get or /config/set endpoint."""
    import requests

    host = _get_agent_host(server_key)
    url = f"http://{host}:{config.API_PORT}/config/{action}"
    try:
        r = requests.post(
            url,
            data=json.dumps(data) if data else json.dumps({}),
            headers={
                "X-DN42-Bot-Api-Secret-Token": config.API_TOKEN,
                "Content-Type": "application/json",
            },
            timeout=10,
        )
        if r.status_code == 200:
            return r.json()
        return {"error": f"HTTP {r.status_code}: {r.text[:200]}"}
    except Exception as e:
        return {"error": str(e)}


def _show_server_list(chat_id, message_id=None):
    """Show the server selection inline keyboard."""
    markup = InlineKeyboardMarkup()
    for key, display in base.servers.items():
        markup.row(InlineKeyboardButton(text=display, callback_data=f"acfg_s_{key}"))
    markup.row(
        InlineKeyboardButton(
            text="Push to All | 下发所有节点", callback_data="acfg_push_all"
        )
    )
    text = (
        "Select a server to view/modify its config, or push a config to all servers.\n"
        "选择一个服务器查看/修改配置，或下发配置到所有服务器。"
    )
    if message_id:
        bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup)
    else:
        bot.send_message(chat_id, text, reply_markup=markup)


def _show_config(chat_id, server_key, message_id=None):
    """Fetch and display config for a server, with editable key buttons."""
    result = _call_agent_config(server_key, "get")
    if "error" in result:
        text = f"Failed to fetch config from `{server_key}`:\n{result['error']}"
        markup = InlineKeyboardMarkup()
        markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="acfg_back"))
        if message_id:
            bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup, parse_mode="Markdown")
        else:
            bot.send_message(chat_id, text, reply_markup=markup, parse_mode="Markdown")
        return

    display = base.servers.get(server_key, server_key)
    config_text = _format_config(result)
    text = f"Config of `{display}`:\n```\n{config_text}\n```"

    markup = InlineKeyboardMarkup()
    for key in EDITABLE_KEYS:
        value = result.get(key, "N/A")
        if isinstance(value, bool):
            value = "T" if value else "F"
        elif isinstance(value, dict):
            value = "{...}"
        label = f"{key}: {value}"
        if len(label) > 35:
            label = label[:32] + "..."
        markup.row(InlineKeyboardButton(text=label, callback_data=f"acfg_k_{server_key}|{key}"))
    markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="acfg_back"))

    if message_id:
        bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup, parse_mode="Markdown")
    else:
        bot.send_message(chat_id, text, reply_markup=markup, parse_mode="Markdown")


def _prompt_value(chat_id, server_key, key, push_all=False):
    """Ask user to enter a new value for a config key."""
    desc = KEY_DESCRIPTIONS.get(key, key)
    prefix = "[Push All] " if push_all else ""
    msg = bot.send_message(
        chat_id,
        f"{prefix}`{key}`\n{desc}\n\nPlease enter a new value, or /cancel:\n请输入新值，或 /cancel：",
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
    bot.register_next_step_handler(
        msg, partial(_apply_value, server_key=server_key, key=key, push_all=push_all)
    )


def _apply_value(message, server_key, key, push_all):
    """Apply the user-entered value to agent(s)."""
    if message.text and message.text.strip() == "/cancel":
        bot.send_message(message.chat.id, "Cancelled.\n已取消。", reply_markup=ReplyKeyboardRemove())
        return

    raw_value = message.text.strip() if message.text else ""

    # Try to parse as JSON for bool/int/dict
    try:
        value = json.loads(raw_value)
    except (json.JSONDecodeError, ValueError):
        value = raw_value

    if push_all:
        _do_push_all(message.chat.id, key, value)
    else:
        _do_set_single(message.chat.id, server_key, key, value)


def _do_set_single(chat_id, server_key, key, value):
    """Set a config value on a single agent."""
    display = base.servers.get(server_key, server_key)
    result = _call_agent_config(server_key, "set", {key: value})

    if "error" in result:
        bot.send_message(
            chat_id,
            f"Failed to set `{key}` on `{display}`:\n{result['error']}",
            parse_mode="Markdown",
        )
        return

    text = f"Config updated on `{display}`:\n"
    for k, v in result.get("updated", {}).items():
        text += f"  `{k}` = `{v}`\n"
    if result.get("restart_required"):
        text += "\n⚠️ Restart required for this change to take effect.\n需要重启才能生效。"

    markup = InlineKeyboardMarkup()
    markup.row(InlineKeyboardButton(text="Back | 返回", callback_data=f"acfg_s_{server_key}"))
    bot.send_message(chat_id, text, parse_mode="Markdown", reply_markup=markup)


def _do_push_all(chat_id, key, value):
    """Push a config value to all online agents."""
    if not base.servers:
        bot.send_message(chat_id, "No servers online.\n没有在线服务器。")
        return

    results = []
    for server_key in base.servers:
        result = _call_agent_config(server_key, "set", {key: value})
        display = base.servers.get(server_key, server_key)
        if "error" in result:
            results.append(f"❌ `{display}`: {result['error']}")
        else:
            restart = " ⚠️" if result.get("restart_required") else ""
            results.append(f"✅ `{display}`{restart}")

    text = f"Push `{key}` = `{value}` to all servers:\n"
    text += "\n".join(results)
    restart_count = sum(1 for r in results if "⚠️" in r)
    if restart_count:
        text += f"\n\n⚠️ {restart_count} server(s) need restart."

    markup = InlineKeyboardMarkup()
    markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="acfg_back"))
    bot.send_message(chat_id, text, parse_mode="Markdown", reply_markup=markup)


# --- Command and Callback Handlers ---


@bot.message_handler(commands=["agent_config"], is_private_chat=True)
def cmd_agent_config(message):
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


@bot.callback_query_handler(func=lambda call: call.data.startswith("acfg_"))
def handle_agent_config_callback(call):
    if call.message.chat.id not in db_privilege:
        bot.answer_callback_query(call.id, "Admin only.", show_alert=True)
        return

    data = call.data

    if data == "acfg_back":
        _show_server_list(call.message.chat.id, call.message.message_id)
        bot.answer_callback_query(call.id)
        return

    if data == "acfg_push_all":
        markup = InlineKeyboardMarkup()
        for key in EDITABLE_KEYS:
            desc = KEY_DESCRIPTIONS.get(key, key)
            label = f"{key}" if len(desc) > 30 else desc
            markup.row(InlineKeyboardButton(text=label, callback_data=f"acfg_p_{key}"))
        markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="acfg_back"))
        bot.edit_message_text(
            "Select a key to push to all servers:\n选择要下发到所有服务器的配置项：",
            chat_id=call.message.chat.id,
            message_id=call.message.message_id,
            reply_markup=markup,
        )
        bot.answer_callback_query(call.id)
        return

    if data.startswith("acfg_s_"):
        server_key = data[7:]
        if server_key not in base.servers:
            bot.answer_callback_query(call.id, "Server offline.", show_alert=True)
            return
        _show_config(call.message.chat.id, server_key, call.message.message_id)
        bot.answer_callback_query(call.id)
        return

    if data.startswith("acfg_k_"):
        # acfg_k_{server_key}|{config_key}
        parts = data[7:].split("|", 1)
        if len(parts) != 2:
            bot.answer_callback_query(call.id, "Invalid.", show_alert=True)
            return
        server_key, key = parts
        if server_key not in base.servers:
            bot.answer_callback_query(call.id, "Server offline.", show_alert=True)
            return
        bot.answer_callback_query(call.id)
        _prompt_value(call.message.chat.id, server_key, key, push_all=False)
        return

    if data.startswith("acfg_p_"):
        key = data[7:]
        bot.answer_callback_query(call.id)
        _prompt_value(call.message.chat.id, None, key, push_all=True)
        return

    bot.answer_callback_query(call.id)
