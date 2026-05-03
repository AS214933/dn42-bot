import json

import base
import config
from base import bot, db_privilege
from telebot.types import InlineKeyboardButton, InlineKeyboardMarkup, ReplyKeyboardRemove

# Config keys to display
DISPLAY_KEYS = [
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


def _format_config(config_data):
    """Format config dict as readable text."""
    lines = []
    for key in DISPLAY_KEYS:
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


def _fetch_agent_config(server_key):
    """Fetch config from an agent."""
    import requests

    host = _get_agent_host(server_key)
    url = f"http://{host}:{config.API_PORT}/config/get"
    try:
        r = requests.post(
            url,
            data=json.dumps({}),
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
    text = "Select a server to view its config.\n选择一个服务器查看配置。"
    if message_id:
        bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup)
    else:
        bot.send_message(chat_id, text, reply_markup=markup)


def _show_config(chat_id, server_key, message_id=None):
    """Fetch and display config for a server."""
    result = _fetch_agent_config(server_key)
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
    markup.row(InlineKeyboardButton(text="Back | 返回", callback_data="acfg_back"))

    if message_id:
        bot.edit_message_text(text, chat_id=chat_id, message_id=message_id, reply_markup=markup, parse_mode="Markdown")
    else:
        bot.send_message(chat_id, text, reply_markup=markup, parse_mode="Markdown")


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

    if data.startswith("acfg_s_"):
        server_key = data[7:]
        if server_key not in base.servers:
            bot.answer_callback_query(call.id, "Server offline.", show_alert=True)
            return
        _show_config(call.message.chat.id, server_key, call.message.message_id)
        bot.answer_callback_query(call.id)
        return

    bot.answer_callback_query(call.id)
