import os
import shutil

import base
import tools
from base import bot
from telebot.types import ReplyKeyboardRemove


@bot.message_handler(commands=["matrix"])
def show_matrix(message):
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
                "Graphviz is not installed on the server, unable to generate matrix diagram.\n"
                "服务器未安装 Graphviz，无法生成矩阵图。\n\n"
                "Please install it: `apt install graphviz`\n请安装：`apt install graphviz`"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    msg = bot.send_message(
        message.chat.id,
        "Generating latency matrix, please wait...\n正在生成节点延迟矩阵图，请稍候...",
        reply_markup=ReplyKeyboardRemove(),
    )

    png_path = tools.get_matrix_graph()
    if not png_path:
        text = "Failed to generate latency matrix.\n生成节点延迟矩阵失败。"
        try:
            bot.edit_message_text(text, chat_id=message.chat.id, message_id=msg.message_id)
        except Exception:
            bot.send_message(message.chat.id, text, reply_markup=ReplyKeyboardRemove())
        return

    try:
        with open(png_path, "rb") as photo:
            bot.send_photo(
                message.chat.id,
                photo=photo,
                caption="Latency Matrix / 节点间延迟矩阵（Ping Avg RTT, ms）",
            )
    except Exception:
        text = "Failed to send matrix image.\n发送矩阵图失败。"
        try:
            bot.edit_message_text(text, chat_id=message.chat.id, message_id=msg.message_id)
        except Exception:
            bot.send_message(message.chat.id, text, reply_markup=ReplyKeyboardRemove())
    else:
        try:
            bot.delete_message(message.chat.id, msg.message_id)
        except Exception:
            pass
    finally:
        try:
            shutil.rmtree(os.path.dirname(png_path), ignore_errors=True)
        except Exception:
            pass
