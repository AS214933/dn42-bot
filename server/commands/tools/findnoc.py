import re

import tools
from base import bot
from commands.tools.whois import whois_raw_query


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


def get_noc_emails(asn):
    """
    获取 ASN 关联的所有 email 地址
    
    支持递归查找：如果 admin-c/tech-c 指向 role/organisation，会继续查找其 admin-c/tech-c
    同时支持 e-mail、abuse-mailbox、contact 等字段
    
    Args:
        asn: AS号
        
    Returns:
        set: email 地址集合
    """
    def extract_emails_from_text(text):
        """从文本中提取所有 email 地址"""
        emails = set()
        for line in text.splitlines():
            if line.startswith("e-mail:") or line.startswith("abuse-mailbox:"):
                email = line.split(":", 1)[1].strip()
                if re.fullmatch(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b", email):
                    emails.add(email)
            elif line.startswith("contact:"):
                # contact 字段也可能包含 email
                email = line.split(":", 1)[1].strip()
                if re.fullmatch(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b", email):
                    emails.add(email)
        return emails
    
    def extract_contacts_from_text(text):
        """从文本中提取 admin-c 和 tech-c"""
        contacts = set()
        for line in text.splitlines():
            if line.startswith("admin-c:") or line.startswith("tech-c:"):
                contact = line.split(":", 1)[1].strip()
                if contact:
                    contacts.add(contact)
        return contacts
    
    def get_contact_text(contact_id):
        """获取 contact 的 whois 信息"""
        return whois_raw_query(contact_id, timeout=3)
    
    def recursive_get_emails(contact_id, visited=None, depth=0):
        """递归获取 contact 及其子 contact 的所有 email"""
        if visited is None:
            visited = set()
        
        # 防止无限递归和循环引用
        if contact_id in visited or depth > 5:
            return set()
        visited.add(contact_id)
        
        contact_text = get_contact_text(contact_id)
        if not contact_text:
            return set()
        
        emails = extract_emails_from_text(contact_text)
        
        # 继续查找子 contact 的 email（即使当前有 email 也继续查找）
        sub_contacts = extract_contacts_from_text(contact_text)
        for sub_contact in sub_contacts:
            emails.update(recursive_get_emails(sub_contact, visited, depth + 1))
        
        return emails
    
    try:
        # 优先本地 registry，兜底远程 whois
        whois_text = whois_raw_query(str(asn), timeout=5)
        
        if not whois_text:
            return set()
        
        # 收集 admin-c 和 tech-c
        contacts = extract_contacts_from_text(whois_text)
        
        if not contacts:
            return set()
        
        # 递归获取所有 email
        emails = set()
        visited = set()
        for contact in contacts:
            emails.update(recursive_get_emails(contact, visited))
        
        return emails
    except BaseException:
        return set()


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

    # 获取 email 列表
    emails = get_noc_emails(asn)

    if emails:
        email_text = "\n".join(f"Email: {email}" for email in sorted(emails))
    else:
        email_text = "Email: Not found / 未找到"

    return (
        f"ASN: AS{asn}\n"
        f"ASN Name: {asn_name}\n"
        f"{email_text}"
    )


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
