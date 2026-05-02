import json
from ipaddress import IPv4Address, IPv6Address

import base
from aiohttp import web
from tools import set_sentry

# Fields that are safe to change at runtime (read fresh on each request)
RUNTIME_SAFE_FIELDS = {
    "OPEN",
    "MAX_PEERS",
    "MIN_PEER_REQUIREMENT",
    "NET_SUPPORT",
    "EXTRA_MSG",
    "DEFAULT_MTU",
    "VNSTAT_AUTO_ADD",
    "VNSTAT_AUTO_REMOVE",
    "BIRD_CTL_PATH",
    "BIRD_TABLE_4",
    "BIRD_TABLE_6",
}

# Fields that require a process restart to take effect
RESTART_REQUIRED_FIELDS = {
    "HOST",
    "PORT",
    "SECRET",
    "SENTRY_DSN",
    "MY_DN42_LINK_LOCAL_ADDRESS",
    "MY_DN42_ULA_ADDRESS",
    "MY_DN42_IPv4_ADDRESS",
    "MY_WG_PUBLIC_KEY",
}

ALL_EDITABLE_FIELDS = RUNTIME_SAFE_FIELDS | RESTART_REQUIRED_FIELDS


def _serialize_value(value):
    """Convert Python objects to JSON-serializable values."""
    if isinstance(value, (IPv4Address, IPv6Address)):
        return str(value)
    return value


def _cast_value(key, value, current_value):
    """Try to cast a string value to match the current value's type."""
    if isinstance(current_value, bool):
        if isinstance(value, bool):
            return value
        if isinstance(value, str):
            return value.lower() in ("true", "1", "yes")
        return bool(value)
    elif isinstance(current_value, int):
        if isinstance(value, int):
            return value
        try:
            return int(value)
        except (ValueError, TypeError):
            raise ValueError(f"Cannot convert '{value}' to int")
    elif isinstance(current_value, dict):
        if isinstance(value, dict):
            return value
        raise ValueError(f"Cannot convert '{type(value).__name__}' to dict")
    else:
        return str(value) if value is not None else ""


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
        requested_keys = list(ALL_EDITABLE_FIELDS)

    result = {}
    for key in requested_keys:
        if key in ALL_EDITABLE_FIELDS:
            result[key] = _serialize_value(getattr(base, key, None))

    return web.json_response(result)


@base.routes.post("/config/set")
@set_sentry
async def config_set(request):
    secret = request.headers.get("X-DN42-Bot-Api-Secret-Token")
    if secret != base.SECRET:
        return web.Response(status=403)

    try:
        updates = await request.json()
    except Exception:
        return web.Response(status=400)

    if not isinstance(updates, dict) or not updates:
        return web.Response(status=400)

    # Validate all keys first
    for key in updates:
        if key not in ALL_EDITABLE_FIELDS:
            return web.json_response(
                {"ok": False, "error": f"Unknown or non-editable field: {key}"},
                status=400,
            )

    # Read current config file
    try:
        with open("agent_config.json", "r") as f:
            config_data = json.load(f)
    except Exception:
        return web.json_response(
            {"ok": False, "error": "Failed to read config file"},
            status=500,
        )

    # Apply updates
    applied = {}
    needs_restart = False
    for key, value in updates.items():
        current_value = config_data.get(key)
        try:
            casted = _cast_value(key, value, current_value)
        except ValueError as e:
            return web.json_response(
                {"ok": False, "error": f"Invalid value for {key}: {e}"},
                status=400,
            )
        config_data[key] = casted
        applied[key] = casted

        # Update runtime global
        setattr(base, key, casted)

        if key in RESTART_REQUIRED_FIELDS:
            needs_restart = True

    # Write config file
    try:
        with open("agent_config.json", "w") as f:
            json.dump(config_data, f, indent=4, ensure_ascii=False)
    except Exception:
        return web.json_response(
            {"ok": False, "error": "Failed to write config file"},
            status=500,
        )

    return web.json_response({
        "ok": True,
        "updated": {k: _serialize_value(v) for k, v in applied.items()},
        "restart_required": needs_restart,
    })
