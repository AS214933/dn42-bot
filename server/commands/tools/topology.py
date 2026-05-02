import os
import shutil

import base
import tools
from base import bot
from telebot.types import ReplyKeyboardRemove


@bot.message_handler(commands=["topology"], is_private_chat=True)
def show_topology(message):
    if not base.servers:
        bot.send_message(
            message.chat.id,
            "No servers are currently online.\n当前没有在线服务器。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    dot_path = shutil.which("dot")
    if not dot_path:
        bot.send_message(
            message.chat.id,
            (
                "Graphviz is not installed on the server, unable to generate topology diagram.\n"
                "服务器未安装 Graphviz，无法生成拓扑图。\n\n"
                "Please install it: `apt install graphviz`\n请安装：`apt install graphviz`"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    msg = bot.send_message(
        message.chat.id,
        "Generating IGP topology diagram, please wait...\n正在生成 IGP 拓扑图，请稍候...",
        reply_markup=ReplyKeyboardRemove(),
    )

    png_path = tools.get_topology_graph()
    if not png_path:
        bot.edit_message_text(
            (
                "Failed to generate topology diagram.\n生成拓扑图失败。\n\n"
                "Possible reasons:\n可能原因：\n"
                "- No Babel configured / no Babel neighbors on agents\n- Agent 节点未配置 Babel / 没有 Babel 邻居\n"
                "- Graphviz rendering error\n- Graphviz 渲染错误"
            ),
            chat_id=message.chat.id,
            message_id=msg.message_id,
        )
        return

    try:
        with open(png_path, "rb") as photo:
            bot.send_photo(
                message.chat.id,
                photo=photo,
                caption="IGP Network Topology / IGP 网络拓扑",
            )
        bot.delete_message(message.chat.id, msg.message_id)
    except Exception:
        bot.edit_message_text(
            "Failed to send topology image.\n发送拓扑图失败。",
            chat_id=message.chat.id,
            message_id=msg.message_id,
        )
    finally:
        # Remove the whole temp directory (DOT + PNG).
        try:
            shutil.rmtree(os.path.dirname(png_path), ignore_errors=True)
        except Exception:
            pass
