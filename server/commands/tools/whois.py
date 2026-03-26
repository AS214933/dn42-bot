import shlex
import string
import subprocess
from ipaddress import ip_network

import base
import config
import tools
from base import bot
from commands.statistics.stats import get_stats
from tools import registry


def get_extra_route(asn):
    route_result = ""
    route = {4: [], 6: []}
    for r in base.AS_ROUTE[asn]:
        net = ip_network(r)
        route[net.version].append((int(net.network_address), net.compressed))
    for ip_version in [4, 6]:
        for _, r in sorted(route[ip_version], key=lambda x: x[0]):
            route_result += f"route{ip_version}:             {r}\n"
    if route_result:
        return f"% Routes for 'AS{asn}':\n{route_result.strip()}"


def _normalize_asn_input(whois_str):
    """
    对简略 ASN 输入做标准化处理。
    
    Args:
        whois_str: 用户输入的查询字符串
        
    Returns:
        标准化后的字符串
    """
    try:
        asn = int(whois_str)
        if asn < 10000:
            return f"424242{asn:04d}"
        elif 20000 <= asn < 30000:
            return f"42424{asn}"
        else:
            return f"{asn}"
    except ValueError:
        return whois_str


def _do_whois_query(whois_str):
    """
    统一的 whois 查询逻辑：本地 registry 优先，远程 whois 兜底。
    
    Args:
        whois_str: 已标准化的查询字符串
        
    Returns:
        (whois_result, found): 查询结果和是否成功标志
    """
    # 1. 尝试本地 registry
    local_result = registry.get_whois_info_from_registry(whois_str)
    if local_result:
        return local_result, True
    
    # 对于 ASN 查询，也尝试加 AS 前缀
    try:
        asn = int(whois_str)
        local_result = registry.get_whois_info_from_registry(f"AS{asn}")
        if local_result:
            return local_result, True
    except ValueError:
        pass
    
    # 2. 尝试远程 whois（如果已配置）
    whois_addr = getattr(config, 'WHOIS_ADDRESS', '')
    if not whois_addr:
        return None, False
    
    try:
        whois_result = (
            subprocess.run(
                shlex.split(f"whois -h {whois_addr} {whois_str}"),
                stdout=subprocess.PIPE,
                timeout=30,
            )
            .stdout.decode("utf-8")
            .strip()
        )
    except subprocess.TimeoutExpired:
        return "Request timeout.\n请求超时。", True
    except BaseException:
        return "Something went wrong.\n发生了一些错误。", True
    
    # 检查是否是有效结果
    if (
        whois_result
        and len(whois_result.splitlines()) > 1
        and "% 404" not in whois_result
        and (
            whois_result.count("Information related to 'inetnum/")
            + whois_result.count("Information related to 'inet6num/")
            != 1
        )
    ):
        return whois_result, True
    
    # 对于 ASN 查询，重试加 AS 前缀
    try:
        asn = int(whois_str)
        if asn < 10000:
            retry_str = f"AS424242{asn:04d}"
        elif 20000 <= asn < 30000:
            retry_str = f"AS42424{asn}"
        else:
            retry_str = f"AS{asn}"
        whois_result = (
            subprocess.run(
                shlex.split(f"whois -h {whois_addr} {retry_str}"),
                stdout=subprocess.PIPE,
                timeout=30,
            )
            .stdout.decode("utf-8")
            .strip()
        )
        if whois_result and len(whois_result.splitlines()) > 1 and "% 404" not in whois_result:
            return whois_result, True
    except (ValueError, subprocess.TimeoutExpired, BaseException):
        pass
    
    # 非 DN42 场景：尝试 whois -I
    if not config.DN42_ONLY:
        try:
            whois_result = (
                subprocess.run(
                    shlex.split(f"whois -I {whois_str}"),
                    stdout=subprocess.PIPE,
                    timeout=30,
                )
                .stdout.decode("utf-8")
                .strip()
            )
            if whois_result:
                return whois_result, True
        except BaseException:
            pass
    
    return whois_result if whois_result else None, bool(whois_result)


def _append_extra_info(whois_result, whois_str):
    """
    为 ASN 查询结果追加 route 和 statistics 信息。
    """
    try:
        asn = int(whois_str[2:]) if whois_str.upper().startswith("AS") else int(whois_str)
        if route_result := get_extra_route(asn):
            whois_result += f"\n\n{route_result}"
        if stats_result := get_stats(asn)[1]:
            whois_result += (
                "\n\n"
                f"% Statistics for 'AS{asn}':\n"
                f'centrality:         {stats_result["centrality"]}\n'
                f'closeness:          {stats_result["closeness"]}\n'
                f'betweenness:        {stats_result["betweenness"]}\n'
                f'peer count:         {stats_result["peer"]}'
            )
    except BaseException:
        pass
    return whois_result


def whois_raw_query(query, timeout=3):
    """
    原始 whois 查询：本地 registry 优先，远程 whois 兜底。
    不做 ASN 标准化，不附加 route/stats 信息。
    供 findnoc / login 等模块直接调用。
    
    Args:
        query: 查询字符串（ASN 如 "AS4242420000"、person、mntner 等）
        timeout: 远程 whois 超时秒数
        
    Returns:
        str or None: whois 结果文本，未找到返回 None
    """
    # 1. 本地 registry
    local_result = registry.get_whois_info_from_registry(query)
    if local_result:
        return local_result
    
    # 2. 远程 whois 兜底
    whois_addr = getattr(config, 'WHOIS_ADDRESS', '')
    if not whois_addr:
        return None
    
    try:
        result = (
            subprocess.run(
                shlex.split(f"whois -h {whois_addr} {query}"),
                stdout=subprocess.PIPE,
                timeout=timeout,
            )
            .stdout.decode("utf-8")
            .strip()
        )
        if result and "% 404" not in result:
            return result
    except BaseException:
        pass
    return None


def whois(whois_str):
    """
    核心 whois 查询函数，供其他指令调用。
    优先查询本地 registry，找不到再查远程 whois，
    若未配置 whois 服务器则仅依赖本地。
    
    Args:
        whois_str: 要查询的字符串（ASN、IP 等）
    
    Returns:
        str: whois 查询结果
    """
    normalized = _normalize_asn_input(whois_str)
    result, found = _do_whois_query(normalized)
    
    if not found or not result:
        result = "Not found in registry.\n在注册表中未找到。"
    
    result = _append_extra_info(result, normalized)
    return result


@bot.message_handler(commands=["whois"])
def another_whois(message):
    """处理用户的 /whois 命令"""
    if len(message.text.split()) < 2:
        bot.reply_to(
            message,
            "Usage: /whois [something]\n用法：/whois [something]",
            reply_markup=tools.gen_peer_me_markup(message),
        )
        return
    whois_str = message.text.split()[1]
    allowed_punctuation = "_-./:"
    if any(c not in (string.ascii_letters + string.digits + allowed_punctuation) for c in whois_str):
        bot.reply_to(
            message,
            (
                "Invalid input.\n"
                "输入无效\n"
                "\n"
                "Only non-empty strings which contain only upper and lower case letters, numbers, spaces and the following special symbols are accepted.\n"
                "只接受仅由大小写英文字母、数字、空格及以下特殊符号组成的非空字符串。\n"
                f"`{allowed_punctuation}`\n"
            ),
            parse_mode="Markdown",
            reply_markup=tools.gen_peer_me_markup(message),
        )
        return
    bot.send_chat_action(chat_id=message.chat.id, action="typing")
    
    # 标准化 ASN 输入
    normalized = _normalize_asn_input(whois_str)
    
    # 统一查询：本地优先，远程兜底
    whois_result, found = _do_whois_query(normalized)
    
    if not found or not whois_result:
        whois_result = "Not found.\n未找到。"
    
    # 追加 ASN 额外信息
    whois_result = _append_extra_info(whois_result, normalized)
    
    if len(whois_result) > 4000:
        whois_result = tools.split_long_msg(whois_result)
        last_msg = message
        for index, m in enumerate(whois_result):
            if index < len(whois_result) - 1:
                last_msg = bot.reply_to(
                    last_msg,
                    f"```WhoisResult\n{m}```To be continued...",
                    parse_mode="Markdown",
                    reply_markup=tools.gen_peer_me_markup(message),
                )
            else:
                bot.reply_to(
                    last_msg,
                    f"```WhoisResult\n{m}```",
                    parse_mode="Markdown",
                    reply_markup=tools.gen_peer_me_markup(message),
                )
    else:
        bot.reply_to(
            message,
            f"```WhoisResult\n{whois_result}```",
            parse_mode="Markdown",
            reply_markup=tools.gen_peer_me_markup(message),
        )
