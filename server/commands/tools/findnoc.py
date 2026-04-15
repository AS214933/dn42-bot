import re

import tools
from base import bot
from commands.tools.whois import whois_raw_query


_EMAIL_PATTERN = re.compile(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}\b")
_TELEGRAM_URL_PATTERN = re.compile(r"(?:https?://)?t\.me/([A-Za-z0-9_]{2,})", re.IGNORECASE)
_TELEGRAM_LABEL_PATTERN = re.compile(r"telegram[^A-Za-z0-9_@]*@?([A-Za-z0-9_]{2,})", re.IGNORECASE)
_IRC_LABEL_PATTERN = re.compile(
    r"\birc\b[^A-Za-z0-9]{0,10}(?:\([^)]*\)\s*)?[^A-Za-z0-9]{0,5}[:>-]",
    re.IGNORECASE,
)
_PHONE_KEYS = {"phone", "telephone", "tel", "fax-no", "mobile"}


def get_asn_name(asn):
    """
    获取 ASN 的名称 (as-name 字段)
    
    Args:
        asn: AS号
        
    Returns:
        str: ASN 名称，如果未找到则返回 None
    """
    try:
        whois_text = whois_raw_query(str(asn), timeout=5)
        
        if whois_text:
            for line in whois_text.splitlines():
                if line.startswith("as-name:"):
                    return line.split(":", 1)[1].strip()
        
        return None
    except BaseException:
        return None


def _parse_kv_line(line):
    if ":" not in line:
        return None, None
    key, value = line.split(":", 1)
    return key.strip().lower(), value.strip()


def _new_contact_info():
    return {"emails": set(), "telegrams": set(), "ircs": set(), "phones": set()}


def _merge_contact_info(target, source):
    for key in target:
        target[key].update(source.get(key, set()))
    return target


def _extract_emails_from_value(value):
    return set(_EMAIL_PATTERN.findall(value))


def _extract_telegram_from_value(value):
    handles = set()
    handles.update(_TELEGRAM_URL_PATTERN.findall(value))
    handles.update(_TELEGRAM_LABEL_PATTERN.findall(value))
    if "telegram" in value.lower():
        handles.update(
            re.findall(r"(?<![A-Za-z0-9._%+-])@([A-Za-z0-9_]{2,})", value)
        )
    return handles


def _extract_irc_from_value(value):
    results = set()
    for match in _IRC_LABEL_PATTERN.finditer(value):
        tail = value[match.end():]
        for part in re.split(r"[|,;/]", tail):
            token = part.strip()
            if not token:
                continue
            tokens = token.split()
            if not tokens:
                continue
            candidate = tokens[0]
            if candidate.startswith("(") and candidate.endswith(")") and len(tokens) > 1:
                candidate = tokens[1]
            candidate = candidate.strip("()[]{}<>.,;")
            if candidate:
                results.add(candidate)
    return results


def _extract_contact_info_from_text(text):
    info = _new_contact_info()
    for line in text.splitlines():
        key, value = _parse_kv_line(line)
        if not key or not value:
            continue

        line_emails = set()
        line_telegrams = set()
        line_ircs = set()
        line_phones = set()

        if key in ("e-mail", "abuse-mailbox", "contact", "remarks", "descr"):
            line_emails.update(_extract_emails_from_value(value))
        if key in ("remarks", "descr", "contact"):
            line_telegrams.update(_extract_telegram_from_value(value))
            line_ircs.update(_extract_irc_from_value(value))
        if key in _PHONE_KEYS:
            line_phones.add(value)

        if key in ("remarks", "descr", "contact") and line_ircs:
            line_emails.difference_update(line_ircs)

        info["emails"].update(line_emails)
        info["telegrams"].update(line_telegrams)
        info["ircs"].update(line_ircs)
        info["phones"].update(line_phones)
    return info


def _extract_contact_ids_from_text(text):
    contacts = set()
    for line in text.splitlines():
        key, value = _parse_kv_line(line)
        if key in ("admin-c", "tech-c", "org") and value:
            contacts.add(value)
    return contacts


def _get_contact_text(contact_id):
    return whois_raw_query(contact_id, timeout=3)


def _recursive_collect_contact_info(contact_id, visited=None, depth=0):
    if visited is None:
        visited = set()
    if contact_id in visited or depth > 5:
        return _new_contact_info()
    visited.add(contact_id)

    contact_text = _get_contact_text(contact_id)
    if not contact_text:
        return _new_contact_info()

    info = _extract_contact_info_from_text(contact_text)
    for sub_contact in _extract_contact_ids_from_text(contact_text):
        _merge_contact_info(info, _recursive_collect_contact_info(sub_contact, visited, depth + 1))
    return info


def get_noc_contacts(asn):
    """
    获取 ASN 关联的 NOC 信息。
    
    支持递归查找 admin-c/tech-c/org，解析 e-mail/abuse-mailbox/contact/remarks/descr/phone。
    
    Args:
        asn: AS号
        
    Returns:
        dict: emails/telegrams/ircs/phones 集合（自动去重）
    """
    try:
        whois_text = whois_raw_query(str(asn), timeout=5)
        if not whois_text:
            return _new_contact_info()

        info = _extract_contact_info_from_text(whois_text)
        contacts = _extract_contact_ids_from_text(whois_text)
        visited = set()
        for contact in contacts:
            _merge_contact_info(info, _recursive_collect_contact_info(contact, visited))
        return info
    except BaseException:
        return _new_contact_info()


def _lookup_single_asn(raw_asn):
    """
    查询单个 ASN 的 NOC 信息。

    Args:
        raw_asn: 用户输入的原始 ASN 字符串

    Returns:
        str: 格式化的查询结果，如果 ASN 无效则返回错误提示
    """
    asn = tools.extract_asn(raw_asn)

    if not asn:
        return f"ASN: {raw_asn}\nError: Invalid ASN or not found / ASN 无效或未找到"

    # 获取 ASN 名称
    asn_name = get_asn_name(asn)
    if not asn_name:
        asn_name = "Unknown"

    contacts = get_noc_contacts(asn)

    def format_lines(label, values, formatter=None, show_when_empty=False):
        if not values:
            return [f"{label}: Not found / 未找到"] if show_when_empty else []
        if formatter is None:
            formatter = lambda value: value
        return [f"{label}: {formatter(value)}" for value in sorted(values)]

    lines = [f"ASN: AS{asn}", f"ASN Name: {asn_name}"]
    lines.extend(format_lines("Email", contacts["emails"], show_when_empty=True))
    lines.extend(
        format_lines(
            "Telegram",
            contacts["telegrams"],
            lambda value: f"@{value}" if not value.startswith("@") else value,
        )
    )
    lines.extend(format_lines("IRC", contacts["ircs"]))
    lines.extend(format_lines("Phone", contacts["phones"]))

    return "\n".join(lines)


@bot.message_handler(commands=["findnoc"])
def findnoc(message):
    """处理 /findnoc 命令，查找一个或多个 ASN 的 NOC 信息"""
    parts = message.text.split()
    if len(parts) < 2:
        bot.reply_to(
            message,
            "Usage: /findnoc [ASN1] [ASN2] ...\n用法：/findnoc [ASN]",
            reply_markup=tools.gen_peer_me_markup(message),
        )
        return

    raw_asns = parts[1:]

    # 限制单次查询数量，防止滥用
    MAX_ASNS = 10
    if len(raw_asns) > MAX_ASNS:
        bot.reply_to(
            message,
            f"Too many ASNs. Maximum {MAX_ASNS} per query.\n"
            f"ASN 数量过多，单次最多查询 {MAX_ASNS} 个。",
            reply_markup=tools.gen_peer_me_markup(message),
        )
        return

    results = [_lookup_single_asn(raw_asn) for raw_asn in raw_asns]
    result = "\n\n".join(results)

    bot.reply_to(
        message,
        result,
        reply_markup=tools.gen_peer_me_markup(message),
    )
