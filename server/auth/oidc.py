import threading

import requests
import config
import tools
from authlib.common.security import generate_token
from authlib.integrations.requests_client import OAuth2Session
from authlib.jose import JsonWebKey, JsonWebToken, jwt
from authlib.oidc.core import CodeIDToken, ImplicitIDToken
from expiringdict import ExpiringDict


IEDON_DISCOVERY_URL = "https://auth.iedon.net/.well-known/openid-configuration"
KIOUBIT_DISCOVERY_URL = "https://dn42.g-load.eu/.well-known/openid-configuration"

PROVIDER_TEMPLATES = {
    "iedon": {
        "display_name": "iEdon Auth42",
        "discovery_url": IEDON_DISCOVERY_URL,
        "scope": "dn42",
        "asn_claim": "dn42.asn",
        "asn_claim_source": "auto",
    },
    "kioubit": {
        "display_name": "Kioubit.dn42",
        "discovery_url": KIOUBIT_DISCOVERY_URL,
        "scope": "dn42",
        "asn_claim": "dn42.asn",
        "asn_claim_source": "auto",
    }
}

_DISCOVERY_CACHE = ExpiringDict(max_len=32, max_age_seconds=3600)
_PENDING_LOGINS = None
_PENDING_LOGINS_TTL = None
_PENDING_LOCK = threading.Lock()


class OIDCError(RuntimeError):
    pass


class OIDCConfigError(OIDCError):
    pass


def _bilingual(en_text, zh_text):
    return f"{en_text}\n{zh_text}"


def _get_oidc_config():
    oidc_config = getattr(config, "OIDC_LOGIN", {}) or {}
    if isinstance(oidc_config, dict):
        return oidc_config
    return {}


def _get_raw_providers():
    providers = _get_oidc_config().get("providers", {}) or {}
    if isinstance(providers, dict):
        return providers
    return {}


def _provider_is_enabled(raw_provider):
    return isinstance(raw_provider, dict) and bool(raw_provider.get("enabled", False))


def get_pending_ttl():
    try:
        ttl = int(_get_oidc_config().get("pending_ttl", 600))
    except (TypeError, ValueError):
        ttl = 600
    return ttl if ttl > 0 else 600


def get_callback_path():
    callback_path = str(_get_oidc_config().get("callback_path") or "/oidc/callback").strip()
    if not callback_path:
        callback_path = "/oidc/callback"
    if not callback_path.startswith("/"):
        callback_path = "/" + callback_path
    return callback_path


def get_callback_url():
    base_url = str(_get_oidc_config().get("base_url") or "").strip()
    if not base_url:
        raise OIDCConfigError(
            _bilingual(
                "External OIDC/OAuth login requires `OIDC_LOGIN['base_url']`.",
                "外部 OIDC/OAuth 登录需要配置 `OIDC_LOGIN['base_url']`。",
            )
        )
    if not (base_url.startswith("http://") or base_url.startswith("https://")):
        raise OIDCConfigError(
            _bilingual(
                "`OIDC_LOGIN['base_url']` must start with `http://` or `https://`.",
                "`OIDC_LOGIN['base_url']` 必须以 `http://` 或 `https://` 开头。",
            )
        )
    return base_url.rstrip("/") + get_callback_path()


def has_enabled_providers():
    return any(_provider_is_enabled(provider) for provider in _get_raw_providers().values())


def get_runtime_error():
    if not has_enabled_providers():
        return _bilingual(
            "External OIDC/OAuth login is not enabled.",
            "当前未启用外部 OIDC/OAuth 登录。",
        )
    if not str(getattr(config, "WEBHOOK_URL", "") or "").strip():
        return _bilingual(
            "External OIDC/OAuth login requires webhook mode. Please configure `WEBHOOK_URL` first.",
            "外部 OIDC/OAuth 登录依赖 webhook 模式，请先配置 `WEBHOOK_URL`。",
        )
    base_url = str(_get_oidc_config().get("base_url") or "").strip()
    if not base_url:
        return _bilingual(
            "External OIDC/OAuth login requires `OIDC_LOGIN['base_url']`.",
            "外部 OIDC/OAuth 登录需要配置 `OIDC_LOGIN['base_url']`。",
        )
    return None


def _get_pending_logins():
    global _PENDING_LOGINS, _PENDING_LOGINS_TTL
    ttl = get_pending_ttl()
    with _PENDING_LOCK:
        if _PENDING_LOGINS is None or _PENDING_LOGINS_TTL != ttl:
            _PENDING_LOGINS = ExpiringDict(max_len=2048, max_age_seconds=ttl)
            _PENDING_LOGINS_TTL = ttl
        return _PENDING_LOGINS


def _normalize_provider(provider_key, raw_provider):
    if not isinstance(raw_provider, dict):
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` must be a dictionary.",
                f"OIDC 提供商 `{provider_key}` 必须是字典配置。",
            )
        )

    template_name = raw_provider.get("template")
    template = {}
    if template_name:
        template = PROVIDER_TEMPLATES.get(template_name)
        if template is None:
            raise OIDCConfigError(
                _bilingual(
                    f"OIDC provider `{provider_key}` uses unknown template `{template_name}`.",
                    f"OIDC 提供商 `{provider_key}` 使用了未知模板 `{template_name}`。",
                )
            )

    provider = dict(template)
    provider.update(raw_provider)
    provider["key"] = provider_key
    provider["display_name"] = str(provider.get("display_name") or provider_key).strip()
    provider["discovery_url"] = str(provider.get("discovery_url") or "").strip()
    provider["client_id"] = str(provider.get("client_id") or "").strip()
    provider["client_secret"] = str(provider.get("client_secret") or "").strip()
    provider["asn_claim"] = str(provider.get("asn_claim") or "").strip()
    provider["asn_claim_source"] = str(provider.get("asn_claim_source") or "").strip().lower()

    scope = str(provider.get("scope") or template.get("scope") or "openid profile").strip()
    scope_items = [item for item in scope.split() if item]
    if "openid" not in scope_items:
        scope_items.insert(0, "openid")
    provider["scope"] = " ".join(dict.fromkeys(scope_items))

    if not provider["discovery_url"]:
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` is missing `discovery_url`.",
                f"OIDC 提供商 `{provider_key}` 缺少 `discovery_url` 配置。",
            )
        )
    if not provider["client_id"]:
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` is missing `client_id`.",
                f"OIDC 提供商 `{provider_key}` 缺少 `client_id` 配置。",
            )
        )
    if not provider["client_secret"]:
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` is missing `client_secret`.",
                f"OIDC 提供商 `{provider_key}` 缺少 `client_secret` 配置。",
            )
        )
    if not provider["asn_claim"]:
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` is missing `asn_claim`.",
                f"OIDC 提供商 `{provider_key}` 缺少 `asn_claim` 配置。",
            )
        )
    if provider["asn_claim_source"] not in {"id_token", "userinfo", "auto"}:
        raise OIDCConfigError(
            _bilingual(
                f"OIDC provider `{provider_key}` must set `asn_claim_source` to `id_token`, `userinfo`, or `auto`.",
                f"OIDC 提供商 `{provider_key}` 必须将 `asn_claim_source` 设置为 `id_token`、`userinfo` 或 `auto`。",
            )
        )

    return provider


def get_provider(provider_key):
    providers = _get_raw_providers()
    if provider_key not in providers:
        raise OIDCError(
            _bilingual(
                f"OIDC provider `{provider_key}` does not exist.",
                f"OIDC 提供商 `{provider_key}` 不存在。",
            )
        )
    raw_provider = providers[provider_key]
    if not _provider_is_enabled(raw_provider):
        raise OIDCError(
            _bilingual(
                f"OIDC provider `{provider_key}` is not enabled.",
                f"OIDC 提供商 `{provider_key}` 未启用。",
            )
        )
    return _normalize_provider(provider_key, raw_provider)


def get_available_providers():
    providers = {}
    for provider_key, raw_provider in _get_raw_providers().items():
        if not _provider_is_enabled(raw_provider):
            continue
        try:
            providers[provider_key] = _normalize_provider(provider_key, raw_provider)
        except OIDCConfigError:
            continue
    return providers


def _get_provider_metadata(provider):
    discovery_url = provider["discovery_url"]
    if discovery_url in _DISCOVERY_CACHE:
        return _DISCOVERY_CACHE[discovery_url]

    try:
        response = requests.get(discovery_url, timeout=10)
        response.raise_for_status()
        metadata = response.json()
    except (ValueError, requests.RequestException) as exc:
        raise OIDCError(
            _bilingual(
                f"Failed to fetch OIDC discovery document from `{discovery_url}`: {exc}",
                f"无法从 `{discovery_url}` 获取 OIDC discovery 文档：{exc}",
            )
        )

    for field in ("issuer", "authorization_endpoint", "token_endpoint"):
        if not metadata.get(field):
            raise OIDCError(
                _bilingual(
                    f"OIDC discovery document for `{provider['display_name']}` is missing `{field}`.",
                    f"`{provider['display_name']}` 的 OIDC discovery 文档缺少 `{field}`。",
                )
            )

    _DISCOVERY_CACHE[discovery_url] = metadata
    return metadata


def _build_oauth_client(provider, metadata):
    client = OAuth2Session(
        provider["client_id"],
        provider["client_secret"],
        scope=provider["scope"],
        redirect_uri=get_callback_url(),
    )
    client.server_metadata = metadata
    return client


def _store_pending_login(state, pending_login):
    pending_logins = _get_pending_logins()
    with _PENDING_LOCK:
        pending_logins[state] = pending_login


def _pop_pending_login(state):
    pending_logins = _get_pending_logins()
    with _PENDING_LOCK:
        return pending_logins.pop(state, None)


def start_login(provider_key, chat_id, asn):
    runtime_error = get_runtime_error()
    if runtime_error:
        raise OIDCError(runtime_error)

    provider = get_provider(provider_key)
    metadata = _get_provider_metadata(provider)
    client = _build_oauth_client(provider, metadata)
    nonce = generate_token(32)
    code_verifier = generate_token(64)

    authorization_url, state = client.create_authorization_url(
        metadata["authorization_endpoint"],
        nonce=nonce,
        code_verifier=code_verifier,
    )
    _store_pending_login(
        state,
        {
            "chat_id": chat_id,
            "asn": asn,
            "provider_key": provider_key,
            "nonce": nonce,
            "code_verifier": code_verifier,
        },
    )
    return {
        "authorization_url": authorization_url,
        "display_name": provider["display_name"],
        "expires_in": get_pending_ttl(),
    }


def _lookup_claim(claims, claim_name):
    if not isinstance(claims, dict):
        return None

    parts = [part.strip() for part in str(claim_name or "").split(".") if part.strip()]
    if not parts:
        return None

    current = claims
    for part in parts:
        if not isinstance(current, dict):
            return None
        if part not in current:
            return None
        current = current.get(part)
    return current


def _fetch_userinfo(metadata, token, claim_name, source):
    if not metadata.get("userinfo_endpoint"):
        if source == "userinfo":
            raise OIDCError(
                _bilingual(
                    "`asn_claim_source` is set to `userinfo`, but the provider metadata does not include `userinfo_endpoint`.",
                    "`asn_claim_source` 被设置为 `userinfo`，但提供商元数据中没有 `userinfo_endpoint`。",
                )
            )
        raise OIDCError(
            _bilingual(
                f"ASN claim `{claim_name}` was not found in the ID token, and the provider metadata does not include `userinfo_endpoint`.",
                f"在 ID token 中未找到 ASN claim `{claim_name}`，且提供商元数据中没有 `userinfo_endpoint`。",
            )
        )
    access_token = token.get("access_token")
    if not access_token:
        raise OIDCError(
            _bilingual(
                "The provider did not return an access token.",
                "提供商没有返回 access token。",
            )
        )
    try:
        response = requests.get(
            metadata["userinfo_endpoint"],
            headers={"Authorization": f"Bearer {access_token}"},
            timeout=10,
        )
        response.raise_for_status()
        return response.json()
    except (ValueError, requests.RequestException) as exc:
        raise OIDCError(
            _bilingual(
                f"Failed to fetch the userinfo response: {exc}",
                f"获取 userinfo 响应失败：{exc}",
            )
        )


def _fetch_jwk_set(provider, metadata, force=False):
    jwk_set = metadata.get("jwks")
    if jwk_set and not force:
        return jwk_set

    jwks_uri = str(metadata.get("jwks_uri") or "").strip()
    if not jwks_uri:
        raise OIDCError(
            _bilingual(
                f"OIDC discovery document for `{provider['display_name']}` is missing `jwks_uri`, which is required to validate the ID token.",
                f"`{provider['display_name']}` 的 OIDC discovery 文档缺少 `jwks_uri`，而校验 ID token 需要它。",
            )
        )

    try:
        response = requests.get(jwks_uri, timeout=10)
        response.raise_for_status()
        jwk_set = response.json()
    except (ValueError, requests.RequestException) as exc:
        raise OIDCError(
            _bilingual(
                f"Failed to fetch the JWK set from `{jwks_uri}`: {exc}",
                f"无法从 `{jwks_uri}` 获取 JWK 集合：{exc}",
            )
        )

    metadata["jwks"] = jwk_set
    return jwk_set


def _create_jwk_load_key(provider, metadata):
    def load_key(header, _payload):
        jwk_set = JsonWebKey.import_key_set(_fetch_jwk_set(provider, metadata))
        try:
            return jwk_set.find_by_kid(header.get("kid"))
        except ValueError:
            refreshed_jwk_set = JsonWebKey.import_key_set(_fetch_jwk_set(provider, metadata, force=True))
            return refreshed_jwk_set.find_by_kid(header.get("kid"))

    return load_key


def _parse_id_token_claims(provider, metadata, token, nonce, claim_source):
    if claim_source not in {"id_token", "auto"}:
        return None
    if not token.get("id_token"):
        if claim_source == "id_token":
            raise OIDCError(
                _bilingual(
                    "`asn_claim_source` is set to `id_token`, but the provider did not return an ID token.",
                    "`asn_claim_source` 被设置为 `id_token`，但提供商没有返回 ID token。",
                )
            )
        return None

    try:
        claims_params = {
            "nonce": nonce,
            "client_id": provider["client_id"],
        }
        if "access_token" in token:
            claims_params["access_token"] = token["access_token"]
            claims_cls = CodeIDToken
        else:
            claims_cls = ImplicitIDToken

        claims_options = None
        if metadata.get("issuer"):
            claims_options = {"iss": {"values": [metadata["issuer"]]}}

        alg_values = metadata.get("id_token_signing_alg_values_supported")
        jwt_decoder = JsonWebToken(alg_values) if alg_values else jwt
        claims = jwt_decoder.decode(
            token["id_token"],
            key=_create_jwk_load_key(provider, metadata),
            claims_cls=claims_cls,
            claims_options=claims_options,
            claims_params=claims_params,
        )

        if claims.get("nonce_supported") is False:
            claims.params["nonce"] = None

        claims.validate(leeway=120)
    except Exception as exc:
        raise OIDCError(
            _bilingual(
                f"Failed to validate the ID token returned by the provider: {exc}",
                f"无法校验提供商返回的 ID token：{exc}",
            )
        )
    if claims is None:
        if claim_source == "id_token":
            raise OIDCError(
                _bilingual(
                    "The provider returned an empty ID token payload.",
                    "提供商返回的 ID token 载荷为空。",
                )
            )
        return None
    return dict(claims)


def _extract_asn_claim(provider, metadata, token, id_token_claims):
    claim_name = provider["asn_claim"]
    claim_source = provider["asn_claim_source"]

    id_token_claim_value = _lookup_claim(id_token_claims, claim_name)
    if claim_source == "id_token":
        if id_token_claim_value is None:
            raise OIDCError(
                _bilingual(
                    f"ASN claim `{claim_name}` was not found in the ID token.",
                    f"在 ID token 中未找到 ASN claim `{claim_name}`。",
                )
            )
        return id_token_claim_value, "id_token"

    if claim_source == "auto" and id_token_claim_value is not None:
        return id_token_claim_value, "id_token"

    userinfo = _fetch_userinfo(metadata, token, claim_name, claim_source)
    userinfo_claim_value = _lookup_claim(userinfo, claim_name)
    if userinfo_claim_value is None:
        if claim_source == "userinfo":
            raise OIDCError(
                _bilingual(
                    f"ASN claim `{claim_name}` was not found in the userinfo response.",
                    f"在 userinfo 响应中未找到 ASN claim `{claim_name}`。",
                )
            )
        raise OIDCError(
            _bilingual(
                f"ASN claim `{claim_name}` was not found in either the ID token or the userinfo response.",
                f"在 ID token 与 userinfo 响应中都未找到 ASN claim `{claim_name}`。",
            )
        )
    return userinfo_claim_value, "userinfo"


def _normalize_claim_asn(claim_name, claim_value):
    if isinstance(claim_value, bool):
        asn = None
    elif isinstance(claim_value, (int, str)):
        asn = tools.extract_asn(str(claim_value))
    elif isinstance(claim_value, dict):
        nested_asn = _lookup_claim(claim_value, "asn")
        if isinstance(nested_asn, (int, str)):
            asn = tools.extract_asn(str(nested_asn))
        else:
            asn = None
    else:
        asn = None

    if not asn:
        raise OIDCError(
            _bilingual(
                f"ASN claim `{claim_name}` could not be parsed as a DN42 ASN: {claim_value!r}",
                f"ASN claim `{claim_name}` 无法解析为 DN42 ASN：{claim_value!r}",
            )
        )
    return asn


def _callback_result(ok, page_title, page_message, chat_id=None, telegram_message=None, asn=None, provider_display_name=None):
    return {
        "ok": ok,
        "page_title": page_title,
        "page_message": page_message,
        "chat_id": chat_id,
        "telegram_message": telegram_message,
        "asn": asn,
        "provider_display_name": provider_display_name,
    }


def finish_login(query_params):
    state = str(query_params.get("state") or "").strip()
    if not state:
        message = _bilingual(
            "The callback request is missing `state`.",
            "回调请求缺少 `state`。",
        )
        return _callback_result(False, "Login failed / 登录失败", message)

    pending_login = _pop_pending_login(state)
    if pending_login is None:
        message = _bilingual(
            "The login state is invalid or has expired. Please restart `/login`.",
            "登录 state 无效或已过期，请重新执行 `/login`。",
        )
        return _callback_result(False, "Login failed / 登录失败", message)

    chat_id = pending_login["chat_id"]
    try:
        provider = get_provider(pending_login["provider_key"])
        provider_name = provider["display_name"]
    except OIDCError as exc:
        return _callback_result(
            False,
            "Login failed / 登录失败",
            str(exc),
            chat_id=chat_id,
            telegram_message=str(exc),
        )

    provider_name = provider["display_name"]
    if query_params.get("error"):
        error_detail = query_params.get("error_description") or query_params.get("error")
        message = _bilingual(
            f"Provider `{provider_name}` rejected the login request: {error_detail}",
            f"提供商 `{provider_name}` 拒绝了此次登录请求：{error_detail}",
        )
        return _callback_result(
            False,
            "Login failed / 登录失败",
            message,
            chat_id=chat_id,
            telegram_message=message,
            provider_display_name=provider_name,
        )

    code = str(query_params.get("code") or "").strip()
    if not code:
        message = _bilingual(
            "The callback request does not contain an authorization code.",
            "回调请求中没有授权码。",
        )
        return _callback_result(
            False,
            "Login failed / 登录失败",
            message,
            chat_id=chat_id,
            telegram_message=message,
            provider_display_name=provider_name,
        )

    try:
        metadata = _get_provider_metadata(provider)
        client = _build_oauth_client(provider, metadata)
        token = client.fetch_token(
            metadata["token_endpoint"],
            code=code,
            redirect_uri=get_callback_url(),
            grant_type="authorization_code",
            code_verifier=pending_login["code_verifier"],
        )
        id_token_claims = _parse_id_token_claims(
            provider,
            metadata,
            token,
            pending_login["nonce"],
            provider["asn_claim_source"],
        )
        claim_value, claim_origin = _extract_asn_claim(provider, metadata, token, id_token_claims)
        oidc_asn = _normalize_claim_asn(provider["asn_claim"], claim_value)
    except OIDCError as exc:
        return _callback_result(
            False,
            "Login failed / 登录失败",
            str(exc),
            chat_id=chat_id,
            telegram_message=str(exc),
            provider_display_name=provider_name,
        )
    except Exception as exc:
        message = _bilingual(
            f"Unexpected error while completing the external login: {exc}",
            f"完成外部登录时出现未预期错误：{exc}",
        )
        return _callback_result(
            False,
            "Login failed / 登录失败",
            message,
            chat_id=chat_id,
            telegram_message=message,
            provider_display_name=provider_name,
        )

    expected_asn = pending_login.get("asn")
    if expected_asn is not None and oidc_asn != expected_asn:
        message = _bilingual(
            (
                f"The ASN returned by provider `{provider_name}` from `{claim_origin}` claim `{provider['asn_claim']}` "
                f"is `{oidc_asn}`, which does not match the ASN you entered `{expected_asn}`."
            ),
            (
                f"提供商 `{provider_name}` 从 `{claim_origin}` 的 claim `{provider['asn_claim']}` 中返回的 ASN 是 `{oidc_asn}`，"
                f"与您输入的 ASN `{expected_asn}` 不一致。"
            ),
        )
        return _callback_result(
            False,
            "Login failed / 登录失败",
            message,
            chat_id=chat_id,
            telegram_message=message,
            provider_display_name=provider_name,
        )

    success_message = _bilingual(
        f"External login via `{provider_name}` has completed successfully. You may return to Telegram now.",
        f"已通过 `{provider_name}` 完成外部登录。现在可以返回 Telegram。",
    )
    return _callback_result(
        True,
        "Login successful / 登录成功",
        success_message,
        chat_id=chat_id,
        asn=oidc_asn,
        provider_display_name=provider_name,
    )
