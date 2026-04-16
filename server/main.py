#!/usr/bin/env python3

import html
import os
import pickle
import re
import time

from auth import oidc
import base
import commands  # noqa: F401
import config
import plugins
import sentry_sdk
import telebot
import tools
import urllib3
from aiohttp import web
from apscheduler.schedulers.background import BackgroundScheduler
from base import bot, db, db_privilege
from commands.user_manage import login as login_command
from pytz import utc
from telebot.handler_backends import BaseMiddleware, CancelUpdate
from telebot.types import BotCommandScopeAllPrivateChats, ReplyKeyboardRemove
import subprocess


class IsPrivateChat(telebot.custom_filters.SimpleCustomFilter):
    key = "is_private_chat"

    @staticmethod
    def check(message):
        is_private = message.chat.type == "private"
        if not is_private:
            bot.reply_to(
                message,
                "This command can only be used in private chat.\n此命令只能在私聊中使用。",
                reply_markup=ReplyKeyboardRemove(),
            )
        return is_private


class IsForMe(telebot.custom_filters.SimpleCustomFilter):
    key = "is_for_me"

    @staticmethod
    def check(message):
        command = message.text.split()[0].split("@")
        if len(command) > 1:
            return command[-1].lower() == bot.get_me().username.lower()
        else:
            return True


class MyMiddleware(BaseMiddleware):
    def __init__(self):
        self.update_types = ["message"]

    def pre_process(self, message, data):
        if not message.text:
            return CancelUpdate()
        command = message.text.split()[0].split("@")
        if len(command) > 1:
            if command[-1].lower() != bot.get_me().username.lower():
                return CancelUpdate()
        if config.SENTRY_DSN and command[0].startswith("/"):
            self.transaction = sentry_sdk.start_transaction(
                name=f"Server {command[0]}",
                op=message.text.strip(),
                sampled=True,
            )
            if message.from_user.username:
                self.transaction.set_tag("username", message.from_user.username)
                sentry_sdk.set_user(
                    {
                        "username": f"{message.from_user.full_name} @{message.from_user.username}",
                        "id": message.from_user.id,
                    }
                )
            else:
                sentry_sdk.set_user(
                    {
                        "username": f"{message.from_user.full_name}",
                        "id": message.from_user.id,
                    }
                )
                sentry_sdk.set_user({"id": message.from_user.id})
            self.transaction.set_tag("user_fullname", message.from_user.full_name)
            self.transaction.set_tag("chat_id", message.chat.id)
            self.transaction.set_tag("chat_type", message.chat.type)
            if message.chat.type == "private":
                if message.chat.id in db_privilege:
                    self.transaction.set_tag("privilege", "True")
                    self.transaction.set_tag("ASN", db[message.chat.id])
                elif message.chat.id in db:
                    self.transaction.set_tag("ASN", db[message.chat.id])
            else:
                if message.chat.title:
                    self.transaction.set_tag("title", message.chat.title)

    def post_process(self, message, data, exception):
        if exception:
            bot.send_message(
                message.chat.id,
                f"Error encountered! Please contact {config.CONTACT}\n遇到错误！请联系 {config.CONTACT}",
                parse_mode="Markdown",
                reply_markup=ReplyKeyboardRemove(),
            )
        try:
            if self.transaction:
                if exception:
                    self.transaction.set_status("error")
                else:
                    self.transaction.set_status("ok")
                self.transaction.finish()
        except BaseException:
            pass


# Startup and initialization
urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

config.CONTACT = re.sub(f'([{re.escape(r"_*`[")}])', r"\\\1", config.CONTACT)

if config.SENTRY_DSN:
    sentry_sdk.init(
        dsn=config.SENTRY_DSN,
        traces_sample_rate=0,
    )

tools.update_china_ip()
tools.update_as_route_table()
tools.servers_check(startup=True)
try:
    data_dir = "./data"
    os.makedirs(data_dir, exist_ok=True)
    with open(os.path.join(data_dir, "map.pkl"), "rb") as f:
        tools.get_map(update=pickle.load(f))
except BaseException:
    tools.get_map(update=True)
if config.FLAPALERTED_URL:
    tools.get_flaps(update=True)


# Setup scheduler
scheduler = BackgroundScheduler(
    timezone=utc,
    job_defaults={"misfire_grace_time": None, "coalesce": True, "replace_existing": True},
)


def scheduler_add_job(func, *args, **kwargs):
    kwargs["trigger"] = "cron"
    kwargs["id"] = "dn42bot_" + func.__name__
    scheduler.add_job(func, *args, **kwargs)


def run_update_registry():
    script_path = os.path.join(os.path.dirname(__file__), "tools", "update_registry.py")
    subprocess.run(["python3", script_path])


scheduler_add_job(tools.servers_check, minute="*/3")
scheduler_add_job(tools.get_map, kwargs={"update": True}, minute="*/3")
scheduler_add_job(tools.update_china_ip, hour="1", minute="30")
scheduler_add_job(tools.update_as_route_table, minute="7/15")
scheduler.add_job(run_update_registry, trigger="cron", minute="*/5", id="dn42bot_update_registry", replace_existing=True)
if config.FLAPALERTED_URL:
    scheduler_add_job(tools.get_flaps, kwargs={"update": True}, minute="*/5")
scheduler.start()


# Setup bot
bot.add_custom_filter(IsPrivateChat())
bot.setup_middleware(MyMiddleware())

cmd_list = {
    "ping": ("Ping IP / Domain", True),
    "tcping": ("TCPing IP / Domain", True),
    "trace": ("Traceroute IP / Domain", True),
    "route": ("Route to IP / Domain", True),
    "path": ("AS-Path of IP / Domain", True),
    "whois": ("Whois", True),
    "dig": ("Dig domain", True),
    "findnoc": ("Find NOC Contacts", True),
    "login": ("Login to verify your ASN 登录以验证你的 ASN", False),
    "logout": ("Logout current logged ASN 退出当前登录的 ASN", False),
    "whoami": ("Get current login user 获取当前登录用户", False),
    "peer": ("Set up a peer 设置一个 Peer", False),
    "modify": ("Modify peer information 修改 Peer 信息", False),
    "remove": ("Remove a peer 移除一个 Peer", False),
    "info": ("Show your peer info and status 查看你的 Peer 信息及状态", False),
    "restart": ("Restart tunnel and bird session 重启隧道及 Bird 会话", False),
    "rank": ("Show DN42 global ranking 显示 DN42 总体排名", True),
    "stats": ("Show DN42 user basic info & statistics 显示 DN42 用户基本信息及数据", True),
    "peer_list": ("Show the peer situation of a user 显示某 DN42 用户的 Peer 情况", True),
}
if config.FLAPALERTED_URL:
    cmd_list["flaps"] = ("Show current flap prefixes 显示当前抖动前缀", True)
cmd_list |= {
    "cancel": ("Cancel ongoing operations 取消正在进行的操作", True),
    "help": ("Get help text 获取帮助文本", True),
}
bot.delete_my_commands()
bot.set_my_commands(
    [telebot.types.BotCommand(cmd, desc) for cmd, (desc, public_available) in cmd_list.items() if public_available]
)
bot.set_my_commands(
    [telebot.types.BotCommand(cmd, desc) for cmd, (desc, _) in cmd_list.items()],
    scope=BotCommandScopeAllPrivateChats(),
)

data_dir = "./data"
os.makedirs(data_dir, exist_ok=True)
bot.enable_save_next_step_handlers(delay=2, filename=os.path.join(data_dir, "step.save"))
bot.load_next_step_handlers(filename=os.path.join(data_dir, "step.save"))

# Load plugins
plugins.load_plugins()
plugin_cmds = {}
for pname, pmod in plugins.get_loaded_plugins().items():
    if hasattr(pmod, "COMMANDS"):
        plugin_cmds.update(pmod.COMMANDS)
if plugin_cmds:
    cmd_list.update(plugin_cmds)
    bot.delete_my_commands()
    bot.set_my_commands(
        [telebot.types.BotCommand(cmd, desc) for cmd, (desc, public_available) in cmd_list.items() if public_available]
    )
    bot.set_my_commands(
        [telebot.types.BotCommand(cmd, desc) for cmd, (desc, _) in cmd_list.items()],
        scope=BotCommandScopeAllPrivateChats(),
    )


bot.remove_webhook()

if config.WEBHOOK_URL:
    time.sleep(0.5)
    WEBHOOK_SECRET = tools.gen_random_code(32)
    bot.set_webhook(url=config.WEBHOOK_URL, secret_token=WEBHOOK_SECRET)

    async def handle(request):
        secret = request.headers.get("X-Telegram-Bot-Api-Secret-Token")
        if secret == WEBHOOK_SECRET:
            request_body_dict = await request.json()
            update = telebot.types.Update.de_json(request_body_dict)
            bot.process_new_updates([update])
            return web.Response()
        else:
            return web.Response(status=403)

    async def health(request):
        return web.Response(body=",".join(base.servers.keys()))

    def render_web_page(title, message):
        safe_title = html.escape(title)
        safe_message = "<br>".join(html.escape(message).splitlines())
        body = (
            "<!DOCTYPE html>"
            "<html><head><meta charset='utf-8'><title>"
            f"{safe_title}"
            "</title><style>body{font-family:sans-serif;max-width:720px;margin:3rem auto;padding:0 1rem;line-height:1.6;}"
            "code{background:#f5f5f5;padding:0.1rem 0.3rem;border-radius:4px;}</style></head>"
            f"<body><h1>{safe_title}</h1><p>{safe_message}</p></body></html>"
        )
        return web.Response(text=body, content_type="text/html")

    async def oidc_webapp_start(request):
        state = str(request.query.get("state") or "").strip()
        if not state:
            message = "The request is missing `state`.\n请求缺少 `state`。"
            return render_web_page("Login failed / 登录失败", message)

        authorization_url = oidc.get_pending_authorization_url(state)
        if not authorization_url:
            message = (
                "The login state is invalid or has expired. Please restart /login.\n"
                "登录 state 无效或已过期，请重新执行 /login。"
            )
            return render_web_page("Login failed / 登录失败", message)

        if not (authorization_url.startswith("http://") or authorization_url.startswith("https://")):
            message = "The authorization URL is invalid.\n授权 URL 无效。"
            return render_web_page("Login failed / 登录失败", message)

        raise web.HTTPFound(authorization_url)

    async def oidc_callback(request):
        callback_result = oidc.finish_login(request.query)
        if callback_result.get("ok"):
            login_command.finish_external_oidc_login(
                callback_result["chat_id"],
                callback_result["asn"],
                callback_result["provider_display_name"],
            )
        elif callback_result.get("chat_id") and callback_result.get("telegram_message"):
            bot.send_message(
                callback_result["chat_id"],
                callback_result["telegram_message"],
                reply_markup=ReplyKeyboardRemove(),
            )
        return render_web_page(callback_result["page_title"], callback_result["page_message"])

    app = web.Application()
    app.router.add_post("/", handle)
    app.router.add_post("/health", health)
    if oidc.has_enabled_providers():
        app.router.add_get(oidc.get_webapp_start_path(), oidc_webapp_start)
        app.router.add_get(oidc.get_callback_path(), oidc_callback)

    # Let plugins mount their web routes onto the aiohttp app
    for pname, pmod in plugins.get_loaded_plugins().items():
        if hasattr(pmod, "setup_web_routes"):
            try:
                pmod.setup_web_routes(app)
            except Exception:
                import traceback
                print(f"[Plugin] Failed to setup web routes for: {pname}")
                traceback.print_exc()

    web.run_app(app, host=config.WEBHOOK_LISTEN_HOST, port=config.WEBHOOK_LISTEN_PORT)

else:
    bot.infinity_polling()
