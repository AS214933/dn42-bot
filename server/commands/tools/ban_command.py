import base
from base import bot, db_privilege, banned_commands, save_banned_commands
from telebot.types import ReplyKeyboardRemove


@bot.message_handler(commands=["ban_command"], is_private_chat=True)
def cmd_ban_command(message):
    if message.chat.id not in db_privilege:
        bot.send_message(
            message.chat.id,
            "This command is for admin only.\n此命令仅限管理员使用。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return

    parts = message.text.split()
    if len(parts) == 1:
        if not banned_commands:
            bot.send_message(
                message.chat.id,
                "No commands are currently banned.\n当前没有禁用的指令。",
                reply_markup=ReplyKeyboardRemove(),
            )
        else:
            bot.send_message(
                message.chat.id,
                "Banned commands:\n已禁用的指令：\n" + "\n".join(f"  /{c}" for c in sorted(banned_commands)),
                reply_markup=ReplyKeyboardRemove(),
            )
        return

    target = parts[1].lower().lstrip("/")
    if target in banned_commands:
        banned_commands.discard(target)
        save_banned_commands()
        base.refresh_bot_commands()
        bot.send_message(
            message.chat.id,
            f"/{target} has been unbanned.\n/{target} 已解除禁用。",
            reply_markup=ReplyKeyboardRemove(),
        )
    else:
        banned_commands.add(target)
        save_banned_commands()
        base.refresh_bot_commands()
        bot.send_message(
            message.chat.id,
            f"/{target} has been banned.\n/{target} 已被禁用。",
            reply_markup=ReplyKeyboardRemove(),
        )
