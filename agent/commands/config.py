from ipaddress import IPv4Address, IPv6Address

import base
from aiohttp import web
from tools import set_sentry

# Fields to expose via /config/get
DISPLAY_FIELDS = {
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
}


def _serialize_value(value):
    """Convert Python objects to JSON-serializable values."""
    if isinstance(value, (IPv4Address, IPv6Address)):
        return str(value)
    return value


@base.routes.post("/config/get")
@set_sentry
async def config_get(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)

    try:
        body = await request.json()
    except Exception:
        body = {}

    requested_keys = body.get("keys", [])
    if not requested_keys:
        requested_keys = list(DISPLAY_FIELDS)

    result = {}
    for key in requested_keys:
        if key in DISPLAY_FIELDS:
            result[key] = _serialize_value(getattr(base, key, None))

    return web.json_response(result)
