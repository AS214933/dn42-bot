import smtplib
from email.mime.multipart import MIMEMultipart
from email.mime.text import MIMEText

BOT_TOKEN = "XXXXX:XXXXXXXXXXXXXXXXXXX"
CONTACT = "@Potat00000"
DN42_ASN = 4242421816

WELCOME_TEXT = (
    f"Hello, I'm the bot for Potat0's DN42 Network (`AS{DN42_ASN}`).\n"
    f"你好，我是 Potat0 (`AS{DN42_ASN}`) 的 DN42 机器人。\n"
    "\n"
    "For more information, please check: 更多信息请查看：\n"
    "https://dn42.potat0.cc/\n"
)

WHOIS_ADDRESS = "127.0.0.1"
DIG_ADDRESS = "172.20.0.53"
DN42_ONLY = False
ALLOW_NO_CLEARNET = True

# API settings
ENDPOINT = "dn42.domain.tld"  # Also used for tunnel
API_PORT = 54321
API_TOKEN = "secret_token"
SERVERS = {
    "las": "LAS | Las Vegas, USA | BuyVM",
    "hkg": "HKG | Hong Kong | Skywolf",
    "trf": "TRF | Sandefjord, Norway | Gigahost",
}
HOSTS = {
    "las": "192.168.1.1",
    "hkg": "hkg.domain.tld",
}

# Servers that require admin (privileged) permission to create new peers.
#
# - Non-privileged users will NOT see these nodes in the /peer node list by default.
# - Users who already have peer info on these nodes can still /modify and /remove,
#   but cannot create new peers or migrate between nodes.
NEED_ADMIN_SERVER = [
    # "las",
]

# Tool nodes visibility control for information commands
# (e.g. /ping, /trace, /route, /tcping, /path).
# Nodes listed here will be completely hidden from the UI and cannot be selected.
TOOLS_HIDDEN_SERVERS = [
    # "trf",
]

# Servers whose WireGuard endpoint should not be revealed to users.
# For these servers, the /info command will display "[ASK_FOR_ENDPOINT]:port"
# instead of the actual hostname, so users must ask the admin for the endpoint.
HIDDEN_ENDPOINT_SERVERS = [
    # "trf",
]

# Webhook settings
WEBHOOK_URL = ""
WEBHOOK_LISTEN_HOST = "127.0.0.1"
WEBHOOK_LISTEN_PORT = 3443

# External OIDC/OAuth login settings
# Requires webhook mode (`WEBHOOK_URL`) because the login flow needs an HTTP callback.
# `base_url` must be the public base URL of this aiohttp service.
OIDC_LOGIN = {
    "base_url": "",
    "callback_path": "/oidc/callback",
    "pending_ttl": 600,
    "providers": {
        "iedon": {
            "enabled": False,
            "template": "iedon",
            "client_id": "your-iedon-client-id",
            "client_secret": "your-iedon-client-secret",
            # Optional overrides:
            # "display_name": "iEdon Auth42",
            # "scope": "dn42",
            # "asn_claim": "dn42.asn",
            # "asn_claim_source": "auto",  # one of: id_token, userinfo, auto
        },
        "example": {
            "enabled": False,
            "display_name": "Example SSO",
            "discovery_url": "https://sso.example.com/.well-known/openid-configuration",
            "client_id": "your-client-id",
            "client_secret": "your-client-secret",
            "scope": "openid profile email",
            "asn_claim": "dn42_asn",
            "asn_claim_source": "userinfo",  # one of: id_token, userinfo, auto
        },
    },
}

# Optional settings
LG_DOMAIN = "https://lg.dn42.domain.tld"
PRIVILEGE_CODE = "123456"
SINGLE_PRIVILEGE = False
FLAPALERTED_URL = "https://flap-dn42.potat0.cc/"
CN_WHITELIST_IP = ["8.8.8.8", "2001:4860:4860::8888"]
SENTRY_DSN = None

# Commands that are disabled for non-admin users.
# Admins can also manage this at runtime via /ban_command.
# Example: ["topology", "trace"]
BANNED_COMMANDS = []

# Plugin system — load plugins from Git repositories
# Each entry: {"git": "<repo_url>", "name": "<plugin_name>", "branch": "<optional_branch>"}
# PLUGINS = [
#     {
#         "git": "https://github.com/yourname/email-plugin.git",
#         "name": "email",
#     },
# ]

# Email-sending function
def send_email(asn, mnt, code, email):
    text = (
        f"Hi {mnt} (AS{asn}),\n"
        "\n"
        "Welcome to my DN42 Network.\n"
        "\n"
        f"Here is your code: {code}\n"
        "\n"
        "Have fun!\n"
    )
    try:
        mimemsg = MIMEMultipart()
        mimemsg["From"] = "My DN42<no-reply@mydomain.tld>"
        mimemsg["To"] = f"{mnt}<{email}>"
        mimemsg["Subject"] = "Verification Code"
        mimemsg.attach(MIMEText(text, "plain"))
        connection = smtplib.SMTP(host="smtp.office365.com", port=587)
        connection.starttls()
        connection.login("no-reply@mydomain.tld", "secret_password")
        connection.send_message(mimemsg)
        connection.quit()
    except BaseException:
        raise RuntimeError
