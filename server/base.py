import os
import pickle
from ipaddress import ip_network

import config
import sentry_sdk
import telebot


class ExceptionHandler(telebot.ExceptionHandler):
    def handle(exception):
        if exception:
            sentry_sdk.capture_exception(exception)


MIN_AGENT_VERSION = 29

bot = telebot.TeleBot(config.BOT_TOKEN, use_class_middlewares=True, exception_handler=ExceptionHandler)

servers = {}

AS_ROUTE = {}
ChinaIPv4 = []
ChinaIPv6 = []
ChinaWhitelist = [ip_network(i) for i in config.CN_WHITELIST_IP]

try:
    data_dir = "./data"
    os.makedirs(data_dir, exist_ok=True)
    with open(os.path.join(data_dir, "user_db.pkl"), "rb") as f:
        db, db_privilege = pickle.load(f)
except BaseException:
    db = {}
    db_privilege = set()

# Banned commands — persisted to ./data/banned_commands.pkl
try:
    with open(os.path.join(data_dir, "banned_commands.pkl"), "rb") as f:
        banned_commands = pickle.load(f)
except BaseException:
    banned_commands = set(getattr(config, "BANNED_COMMANDS", []) or [])

# Command list — populated by main.py, used by refresh_bot_commands()
cmd_list = {}


def save_banned_commands():
    os.makedirs(data_dir, exist_ok=True)
    with open(os.path.join(data_dir, "banned_commands.pkl"), "wb") as f:
        pickle.dump(banned_commands, f)


def refresh_bot_commands():
    from telebot.types import BotCommand, BotCommandScopeAllPrivateChats

    visible = {c: (d, p) for c, (d, p) in cmd_list.items() if c not in banned_commands}
    bot.delete_my_commands()
    bot.set_my_commands(
        [BotCommand(c, d) for c, (d, p) in visible.items() if p]
    )
    bot.set_my_commands(
        [BotCommand(c, d) for c, (d, _) in visible.items()],
        scope=BotCommandScopeAllPrivateChats(),
    )
