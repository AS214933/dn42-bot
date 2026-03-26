import os
import pickle
import re
import shlex
import subprocess
from functools import partial

import config
import tools
from base import bot, db, db_privilege
from commands.tools.whois import whois_raw_query
from telebot.types import KeyboardButton, ReplyKeyboardMarkup, ReplyKeyboardRemove


def get_email(asn):
    """获取 ASN 关联的所有 email 地址
    
    支持递归查找：如果 admin-c 指向 role/organisation，会继续查找其 admin-c/tech-c
    
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
        
        # 如果当前 contact 没有 email，继续查找其 admin-c/tech-c
        if not emails:
            sub_contacts = extract_contacts_from_text(contact_text)
            for sub_contact in sub_contacts:
                emails.update(recursive_get_emails(sub_contact, visited, depth + 1))
        
        return emails
    
    try:
        # 优先本地 registry，兜底远程 whois
        whois_text = whois_raw_query(str(asn), timeout=5)
        
        if not whois_text:
            return set()
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


def get_auth(asn):
    """获取 ASN 对应的 mntner 的认证方式 (auth)
    
    auth 字段存储在 mntner 文件中，而非 aut-num 文件中。
    需要先从 aut-num 获取 mnt-by，然后从 mntner 获取 auth。
    """
    try:
        # 首先获取 ASN 的 mnt-by 字段
        whois_text = whois_raw_query(str(asn), timeout=5)
        
        if not whois_text:
            return set()
        
        whois = whois_text.splitlines()
        
        # 从 ASN 信息中获取 mnt-by
        mnt_by = None
        for line in whois:
            if line.startswith("mnt-by:"):
                mnt_by = line.split(":")[1].strip()
                break
        
        if not mnt_by:
            return set()
        
        # 从 mntner 获取 auth 字段
        mntner_text = whois_raw_query(mnt_by, timeout=5)
        
        if not mntner_text:
            return set()
        
        mntner_whois = mntner_text.splitlines()
        
        auths = set()
        for line in mntner_whois:
            if line.startswith("auth:"):
                # auth 字段格式可能是 "auth: ssh-ed25519 AAAA..." 或 "auth: pgpkey-XXXX"
                # 需要保留完整的值（冒号后的所有内容）
                auth = line.split(":", 1)[1].strip()
                auths.add(auth)
        return auths
    except BaseException:
        return set()

@bot.message_handler(commands=["login"], is_private_chat=True)
def start_login(message):
    if message.chat.id in db:
        bot.send_message(
            message.chat.id,
            (
                f"You are already logged in as `{tools.get_asn_mnt_text(db[message.chat.id])}`, please use /logout to log out.\n"
                f"你已经以 `{tools.get_asn_mnt_text(db[message.chat.id])}` 的身份登录了，请使用 /logout 退出。"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    if len(message.text.split()) == 2:
        login_input_asn(message.text.split()[1], message)
        return
    msg = bot.send_message(
        message.chat.id,
        "Enter your ASN\n请输入你的 ASN",
        reply_markup=ReplyKeyboardRemove(),
    )
    bot.register_next_step_handler(msg, partial(login_input_asn, None))


def login_input_asn(exist_asn, message):
    raw = str(exist_asn) if exist_asn else message.text.strip()
    if raw == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    if asn := tools.extract_asn(raw):
        # 进入验证方式选择
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        markup.add(
            KeyboardButton("📧 Email Verification 邮箱验证"),
            KeyboardButton("🔐 Signature Challenge 签名挑战")
        )
        msg = bot.send_message(
            message.chat.id,
            (
                "Choose authentication method. Use /cancel to interrupt the operation.\n"
                "选择验证方式。使用 /cancel 终止操作。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_auth_method, asn))
    elif exist_asn:
        msg = bot.send_message(
            message.chat.id,
            "Input is not a registered DN42 ASN, please try again.\n输入不是已注册的 DN42 ASN，请重试。",
            reply_markup=ReplyKeyboardRemove(),
        )
    else:
        msg = bot.send_message(
            message.chat.id,
            (
                "Input is not a registered DN42 ASN, please try again. Use /cancel to interrupt the operation.\n"
                "输入不是已注册的 DN42 ASN，请重试。使用 /cancel 终止操作。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )
        bot.register_next_step_handler(msg, partial(login_input_asn, None))


def login_choose_auth_method(asn, message):
    """选择验证方式"""
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    choice = message.text.strip()
    
    # 邮箱验证
    if "Email" in choice or "邮箱" in choice:
        login_start_email_verification(asn, message)
    # 签名挑战（自动检测GPG/SSH）
    elif "Signature" in choice or "签名" in choice:
        login_signature_challenge(asn, message)
    else:
        msg = bot.send_message(
            message.chat.id,
            (
                "Invalid choice. Please select a valid authentication method. Use /cancel to interrupt the operation.\n"
                "无效的选择。请选择有效的验证方式。使用 /cancel 终止操作。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )
        bot.register_next_step_handler(msg, partial(login_choose_auth_method, asn))


def login_start_email_verification(asn, message):
    """开始邮箱验证流程"""
    emails = get_email(asn)

    markup = ReplyKeyboardMarkup(resize_keyboard=True)
    markup.row_width = 1
    if emails:
        markup.add(*(KeyboardButton(email) for email in emails))
    markup.add(KeyboardButton("None of the above 以上都不是"))
    msg = bot.send_message(
        message.chat.id,
        (
            "Select the email address to receive the verification code. Use /cancel to interrupt the operation.\n"
            "选择接收验证码的邮箱。使用 /cancel 终止操作。"
        ),
        reply_markup=markup,
    )
    bot.register_next_step_handler(msg, partial(login_choose_email, asn, emails, msg.message_id))


def login_signature_challenge(asn, message):
    """签名挑战验证
    
    自动从 registry 中获取用户的认证方式（GPG/SSH）
    
    Args:
        asn: AS号
        message: 消息对象
    """
    # 检查 PRIVILEGE_CODE
    if (
        config.PRIVILEGE_CODE
        and (not (config.SINGLE_PRIVILEGE and db_privilege))
        and message.text.strip() == config.PRIVILEGE_CODE
    ):
        db[message.chat.id] = asn
        db_privilege.add(message.chat.id)
        data_dir = "./data"
        os.makedirs(data_dir, exist_ok=True)
        with open(os.path.join(data_dir, "user_db.pkl"), "wb") as f:
            pickle.dump((db, db_privilege), f)
        bot.send_message(
            message.chat.id,
            (
                "*[Privilege]*\n"
                f"Welcome! `{tools.get_asn_mnt_text(asn)}`\n"
                f"欢迎你！`{tools.get_asn_mnt_text(asn)}`"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    # 获取认证方式
    auths = get_auth(asn)
    
    if not auths:
        bot.send_message(
            message.chat.id,
            (
                "No authentication method found in the registry.\n"
                "在 Registry 中未找到认证方式。\n"
                "\n"
                "Please use email verification instead. You can use /login to try again.\n"
                "请改用邮箱验证。你可以使用 /login 重试。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    # 解析认证方式，查找 GPG 指纹和 SSH 密钥
    gpg_fingerprints = []
    ssh_keys = []
    
    for auth in auths:
        auth_upper = auth.upper()
        if auth_upper.startswith("PGPKEY-"):
            # GPG 格式1: pgpkey-<fingerprint>
            fingerprint = auth[7:]  # 去掉 "pgpkey-" 前缀
            gpg_fingerprints.append(fingerprint)
        elif auth_upper.startswith("PGP-FINGERPRINT "):
            # GPG 格式2: pgp-fingerprint <fingerprint>
            fingerprint = auth[16:]  # 去掉 "pgp-fingerprint " 前缀
            gpg_fingerprints.append(fingerprint)
        elif auth_upper.startswith("SSH-"):
            # SSH 格式: ssh-<algo> <key>
            ssh_keys.append(auth)
    
    if not gpg_fingerprints and not ssh_keys:
        bot.send_message(
            message.chat.id,
            (
                "No GPG or SSH authentication found in the registry.\n"
                "在 Registry 中未找到 GPG 或 SSH 认证方式。\n"
                "\n"
                "Please use email verification instead. You can use /login to try again.\n"
                "请改用邮箱验证。你可以使用 /login 重试。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    # 如果同时有 GPG 和 SSH，让用户选择
    if gpg_fingerprints and ssh_keys:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        markup.add(
            KeyboardButton("🔐 GPG Signature GPG 签名"),
            KeyboardButton("🔑 SSH Signature SSH 签名")
        )
        msg = bot.send_message(
            message.chat.id,
            (
                "Multiple authentication methods found. Please choose one.\n"
                "发现多种认证方式，请选择一种。\n"
                "\n"
                "Use /cancel to interrupt the operation.\n"
                "使用 /cancel 终止操作。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_signature_type, asn, gpg_fingerprints, ssh_keys))
    elif gpg_fingerprints:
        # 只有 GPG
        login_start_gpg_challenge(asn, gpg_fingerprints, message)
    else:
        # 只有 SSH
        login_start_ssh_challenge(asn, ssh_keys, message)


def login_choose_signature_type(asn, gpg_fingerprints, ssh_keys, message):
    """选择签名类型（GPG 或 SSH）"""
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    choice = message.text.strip()
    
    if "GPG" in choice:
        login_start_gpg_challenge(asn, gpg_fingerprints, message)
    elif "SSH" in choice:
        login_start_ssh_challenge(asn, ssh_keys, message)
    else:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        markup.add(
            KeyboardButton("🔐 GPG Signature GPG 签名"),
            KeyboardButton("🔑 SSH Signature SSH 签名")
        )
        msg = bot.send_message(
            message.chat.id,
            (
                "Invalid choice. Please select GPG or SSH. Use /cancel to interrupt.\n"
                "无效的选择。请选择 GPG 或 SSH。使用 /cancel 终止操作。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_signature_type, asn, gpg_fingerprints, ssh_keys))


def login_start_gpg_challenge(asn, gpg_fingerprints, message):
    """开始 GPG 签名挑战"""
    # 如果有多个 GPG 指纹，让用户选择
    if len(gpg_fingerprints) > 1:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        for fp in gpg_fingerprints:
            # 显示指纹的前8位和后8位，方便识别
            display_fp = f"{fp[:8]}...{fp[-8:]}" if len(fp) > 16 else fp
            markup.add(KeyboardButton(f"🔐 {display_fp}"))
        
        fingerprint_list = "\n".join([f"- `{fp}`" for fp in gpg_fingerprints])
        msg = bot.send_message(
            message.chat.id,
            (
                "Multiple GPG keys found. Please choose one.\n"
                "发现多个 GPG 密钥，请选择一个。\n"
                "\n"
                f"Available fingerprints / 可用指纹：\n"
                f"{fingerprint_list}\n"
                "\n"
                "Use /cancel to interrupt the operation.\n"
                "使用 /cancel 终止操作。"
            ),
            parse_mode="Markdown",
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_gpg_key, asn, gpg_fingerprints))
    else:
        # 只有一个指纹，直接开始挑战
        login_do_gpg_challenge(asn, gpg_fingerprints[0], message)


def login_choose_gpg_key(asn, gpg_fingerprints, message):
    """选择 GPG 密钥"""
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    choice = message.text.strip()
    
    # 从选择中提取指纹
    selected_fp = None
    for fp in gpg_fingerprints:
        display_fp = f"{fp[:8]}...{fp[-8:]}" if len(fp) > 16 else fp
        if display_fp in choice or fp in choice:
            selected_fp = fp
            break
    
    if selected_fp:
        login_do_gpg_challenge(asn, selected_fp, message)
    else:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        for fp in gpg_fingerprints:
            display_fp = f"{fp[:8]}...{fp[-8:]}" if len(fp) > 16 else fp
            markup.add(KeyboardButton(f"🔐 {display_fp}"))
        
        msg = bot.send_message(
            message.chat.id,
            (
                "Invalid choice. Please select a GPG key. Use /cancel to interrupt.\n"
                "无效的选择。请选择一个 GPG 密钥。使用 /cancel 终止操作。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_gpg_key, asn, gpg_fingerprints))


def login_do_gpg_challenge(asn, gpg_fingerprint, message):
    """执行 GPG 签名挑战"""
    challenge = tools.gen_random_code(32)
    
    msg = bot.send_message(
        message.chat.id,
        (
            "🔐 GPG Signature Challenge\n"
            "🔐 GPG 签名挑战\n"
            "\n"
            f"Selected GPG Fingerprint / 选择的 GPG 指纹：\n"
            f"- `{gpg_fingerprint}`\n"
            "\n"
            f"Challenge String / 挑战字符串:\n"
            f"`{challenge}`\n"
            "\n"
            "Please sign the challenge string with your GPG private key and send the signature.\n"
            "请使用你的 GPG 私钥对挑战字符串进行签名，并发送签名结果。\n"
            "\n"
            "Command / 命令:\n"
            f"`echo -n '{challenge}' | gpg --clearsign`\n"
            "\n"
            "Send the complete signed message (including headers). Use /cancel to interrupt.\n"
            "发送完整的签名消息（包括头部）。使用 /cancel 终止操作。"
        ),
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
    # 只用选择的指纹进行验证
    bot.register_next_step_handler(msg, partial(login_signature_verify_gpg, asn, challenge, [gpg_fingerprint]))


def login_start_ssh_challenge(asn, ssh_keys, message):
    """开始 SSH 签名挑战"""
    # 如果有多个 SSH 密钥，让用户选择
    if len(ssh_keys) > 1:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        for i, key in enumerate(ssh_keys):
            # 提取密钥类型和部分指纹
            parts = key.split()
            key_type = parts[0] if parts else "ssh"
            # 显示密钥类型和公钥的前20个字符
            key_preview = parts[1][:20] + "..." if len(parts) > 1 and len(parts[1]) > 20 else (parts[1] if len(parts) > 1 else "")
            markup.add(KeyboardButton(f"🔑 {i+1}. {key_type} {key_preview}"))
        
        ssh_key_list = "\n".join([f"{i+1}. `{key[:60]}...`" if len(key) > 60 else f"{i+1}. `{key}`" for i, key in enumerate(ssh_keys)])
        msg = bot.send_message(
            message.chat.id,
            (
                "Multiple SSH keys found. Please choose one.\n"
                "发现多个 SSH 密钥，请选择一个。\n"
                "\n"
                f"Available keys / 可用密钥：\n"
                f"{ssh_key_list}\n"
                "\n"
                "Use /cancel to interrupt the operation.\n"
                "使用 /cancel 终止操作。"
            ),
            parse_mode="Markdown",
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_ssh_key, asn, ssh_keys))
    else:
        # 只有一个密钥，直接开始挑战
        login_do_ssh_challenge(asn, ssh_keys[0], message)


def login_choose_ssh_key(asn, ssh_keys, message):
    """选择 SSH 密钥"""
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    choice = message.text.strip()
    
    # 从选择中提取序号
    selected_key = None
    for i, key in enumerate(ssh_keys):
        if f"{i+1}." in choice or f"{i+1}. " in choice:
            selected_key = key
            break
        # 也检查密钥内容匹配
        parts = key.split()
        if len(parts) > 1 and parts[1][:20] in choice:
            selected_key = key
            break
    
    if selected_key:
        login_do_ssh_challenge(asn, selected_key, message)
    else:
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        for i, key in enumerate(ssh_keys):
            parts = key.split()
            key_type = parts[0] if parts else "ssh"
            key_preview = parts[1][:20] + "..." if len(parts) > 1 and len(parts[1]) > 20 else (parts[1] if len(parts) > 1 else "")
            markup.add(KeyboardButton(f"🔑 {i+1}. {key_type} {key_preview}"))
        
        msg = bot.send_message(
            message.chat.id,
            (
                "Invalid choice. Please select an SSH key. Use /cancel to interrupt.\n"
                "无效的选择。请选择一个 SSH 密钥。使用 /cancel 终止操作。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(msg, partial(login_choose_ssh_key, asn, ssh_keys))


def login_do_ssh_challenge(asn, ssh_key, message):
    """执行 SSH 签名挑战"""
    challenge = tools.gen_random_code(32)
    
    # 格式化显示密钥
    ssh_key_display = f"`{ssh_key[:60]}...`" if len(ssh_key) > 60 else f"`{ssh_key}`"
    
    msg = bot.send_message(
        message.chat.id,
        (
            "🔑 SSH Signature Challenge\n"
            "🔑 SSH 签名挑战\n"
            "\n"
            f"Selected SSH Public Key / 选择的 SSH 公钥：\n"
            f"- {ssh_key_display}\n"
            "\n"
            f"Challenge String / 挑战字符串:\n"
            f"`{challenge}`\n"
            "\n"
            "Please sign the challenge string with your SSH private key and send the signature.\n"
            "请使用你的 SSH 私钥对挑战字符串进行签名，并发送签名结果。\n"
            "\n"
            "Command / 命令:\n"
            f"`echo -n '{challenge}' | ssh-keygen -Y sign -f ~/.ssh/id_ed25519 -n file`\n"
            "\n"
            "Send the output signature. Use /cancel to interrupt.\n"
            "发送输出的签名内容。使用 /cancel 终止操作。"
        ),
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )
    # 只用选择的密钥进行验证
    bot.register_next_step_handler(msg, partial(login_signature_verify_ssh, asn, challenge, [ssh_key]))


def _extract_fingerprint_from_gpg_output(stderr):
    """从 GPG 输出中提取主密钥指纹
    
    注意：GPG 签名可能使用子密钥(subkey)，但 DN42 registry 中注册的是主密钥指纹。
    因此需要优先提取主密钥指纹(Primary key fingerprint)。
    
    Args:
        stderr: GPG 命令的 stderr 输出
        
    Returns:
        str or None: 提取到的主密钥指纹（大写，无空格），或 None
    """
    primary_fingerprint = None
    subkey_fingerprint = None
    
    for line in stderr.split('\n'):
        # 优先提取主密钥指纹
        if 'Primary key fingerprint:' in line:
            parts = line.split(':')
            if len(parts) > 1:
                primary_fingerprint = parts[-1].strip().replace(' ', '').upper()
        # 子密钥指纹作为备选
        elif 'Subkey fingerprint:' in line:
            parts = line.split(':')
            if len(parts) > 1:
                subkey_fingerprint = parts[-1].strip().replace(' ', '').upper()
        # 其他格式的指纹提取（如果没有明确标识）
        elif 'fingerprint:' in line.lower() and not primary_fingerprint:
            parts = line.split(':')
            if len(parts) > 1:
                primary_fingerprint = parts[-1].strip().replace(' ', '').upper()
        # GPG 输出中也可能是 "using RSA key XXXX" 格式（这是子密钥）
        elif 'using' in line.lower() and 'key' in line.lower() and not subkey_fingerprint:
            words = line.split()
            for word in words:
                if len(word) >= 16 and all(c in '0123456789ABCDEFabcdef' for c in word):
                    subkey_fingerprint = word.upper()
    
    # 优先返回主密钥指纹，如果没有则返回子密钥指纹
    return primary_fingerprint or subkey_fingerprint


def _gpg_decrypt_challenge(temp_file):
    """使用 GPG 解密签名消息，获取原文
    
    Args:
        temp_file: 签名文件路径
        
    Returns:
        tuple: (decrypted_text: str or None, stderr: str)
    """
    try:
        decrypt_result = subprocess.run(
            ['gpg', '--decrypt', temp_file],
            capture_output=True,
            text=True,
            timeout=10
        )
        return decrypt_result.stdout.strip(), decrypt_result.stderr
    except Exception as e:
        return None, str(e)


def _try_gpg_verify_fingerprint(temp_file, gpg_fingerprints):
    """验证 GPG 签名的指纹是否匹配
    
    Args:
        temp_file: 签名文件路径
        gpg_fingerprints: 允许的 GPG 指纹列表
        
    Returns:
        tuple: (success: bool, signature_fingerprint: str or None, error_msg: str or None)
    """
    # 使用 gpg --verify 验证签名
    result = subprocess.run(
        ['gpg', '--verify', temp_file],
        capture_output=True,
        text=True,
        timeout=10
    )
    
    stderr = result.stderr
    signature_fingerprint = _extract_fingerprint_from_gpg_output(stderr)
    
    if not signature_fingerprint:
        return False, None, "Could not extract fingerprint from signature"
    
    # 验证签名的指纹是否在注册的指纹列表中
    fingerprints_upper = [fp.replace(' ', '').upper() for fp in gpg_fingerprints]
    
    fingerprint_matched = any(
        sig_fp in fp or fp in sig_fp 
        for sig_fp in [signature_fingerprint] 
        for fp in fingerprints_upper
    )
    
    if not fingerprint_matched:
        return False, signature_fingerprint, "Fingerprint not matched"
    
    return True, signature_fingerprint, None


def _recv_gpg_key_from_keyserver(fingerprint, keyserver):
    """从密钥服务器获取 GPG 密钥
    
    Args:
        fingerprint: 密钥指纹
        keyserver: 密钥服务器地址
        
    Returns:
        bool: 是否成功获取
    """
    try:
        result = subprocess.run(
            ['gpg', '--keyserver', keyserver, '--recv-keys', fingerprint],
            capture_output=True,
            text=True,
            timeout=30
        )
        return result.returncode == 0
    except Exception:
        return False


def _import_gpg_key_from_text(key_text):
    """从文本导入 GPG 公钥
    
    Args:
        key_text: GPG 公钥文本（ASCII armored 格式）
        
    Returns:
        tuple: (success: bool, fingerprint: str or None, error_msg: str or None)
    """
    try:
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.asc', delete=False) as f:
            f.write(key_text)
            key_file = f.name
        
        try:
            # 导入公钥
            result = subprocess.run(
                ['gpg', '--import', key_file],
                capture_output=True,
                text=True,
                timeout=10
            )
            
            # 从输出中提取指纹
            stderr = result.stderr
            fingerprint = None
            
            # 尝试从 "key XXXX: public key" 格式提取
            for line in stderr.split('\n'):
                if 'key' in line.lower() and ':' in line:
                    words = line.split()
                    for i, word in enumerate(words):
                        if word.lower() == 'key' and i + 1 < len(words):
                            potential_fp = words[i + 1].rstrip(':').upper()
                            if len(potential_fp) >= 8 and all(c in '0123456789ABCDEF' for c in potential_fp):
                                fingerprint = potential_fp
                                break
                    if fingerprint:
                        break
            
            if result.returncode == 0:
                return True, fingerprint, None
            else:
                return False, None, f"Import failed: {stderr}"
                
        finally:
            os.unlink(key_file)
            
    except Exception as e:
        return False, None, str(e)


def _do_gpg_login_success(chat_id, asn):
    """GPG 登录成功后的处理"""
    db[chat_id] = asn
    data_dir = "./data"
    os.makedirs(data_dir, exist_ok=True)
    with open(os.path.join(data_dir, "user_db.pkl"), "wb") as f:
        pickle.dump((db, db_privilege), f)
    
    bot.send_message(
        chat_id,
        (
            f"✅ Signature verified successfully!\n"
            f"✅ 签名验证成功！\n"
            "\n"
            f"Welcome! `{tools.get_asn_mnt_text(asn)}`\n"
            f"欢迎你！`{tools.get_asn_mnt_text(asn)}`"
        ),
        parse_mode="Markdown",
        reply_markup=ReplyKeyboardRemove(),
    )


def login_signature_verify_gpg(asn, challenge, gpg_fingerprints, message):
    """验证 GPG 签名
    
    验证流程：
    1. 先运行 gpg --decrypt 验证挑战字符串是否匹配
    2. 尝试直接验证指纹（使用本地密钥）
    3. 如果指纹验证失败，尝试从密钥服务器获取密钥后再验证
    4. 如果仍然失败，询问用户是否手动上传公钥
    
    Args:
        asn: AS号
        challenge: 挑战字符串
        gpg_fingerprints: GPG 指纹列表
        message: 消息对象（包含用户发送的签名）
    """
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    signed_message = message.text.strip()
    
    # 验证签名
    try:
        # 将签名消息写入临时文件
        import tempfile
        with tempfile.NamedTemporaryFile(mode='w', suffix='.asc', delete=False) as f:
            f.write(signed_message)
            temp_file = f.name
        
        try:
            # 第一步：先验证挑战字符串是否匹配
            decrypted_text, decrypt_stderr = _gpg_decrypt_challenge(temp_file)
            
            if decrypted_text is None or challenge not in decrypted_text:
                bot.send_message(
                    message.chat.id,
                    (
                        "❌ Challenge string mismatch!\n"
                        "❌ 挑战字符串不匹配！\n"
                        "\n"
                        f"Expected / 期望: `{challenge}`\n"
                        f"Got / 收到: `{decrypted_text if decrypted_text else '(unable to decrypt)'}`\n"
                        "\n"
                        "Please make sure you signed the correct challenge string.\n"
                        "请确保你签名了正确的挑战字符串。\n"
                        "\n"
                        "You can use /login to retry.\n"
                        "你可以使用 /login 重试。"
                    ),
                    parse_mode="Markdown",
                    reply_markup=ReplyKeyboardRemove(),
                )
                return
            
            # 第二步：尝试直接验证指纹
            success, signature_fingerprint, error_msg = _try_gpg_verify_fingerprint(
                temp_file, gpg_fingerprints
            )
            
            if success:
                _do_gpg_login_success(message.chat.id, asn)
                return
            
            # 第三步：尝试从密钥服务器获取用户选择的指纹对应的公钥
            # 提示用户等待
            wait_msg = bot.send_message(
                message.chat.id,
                (
                    "⏳ Local verification failed. Trying to fetch the public key from keyservers...\n"
                    "⏳ 本地验证失败，正在尝试从密钥服务器获取公钥...\n"
                    "\n"
                    "This may take a while (up to 1 minute), please be patient.\n"
                    "这可能需要一些时间（最多 1 分钟），请耐心等待。"
                ),
                reply_markup=ReplyKeyboardRemove(),
            )
            bot.send_chat_action(chat_id=message.chat.id, action="typing")
            
            # 尝试从密钥服务器获取密钥（使用用户选择的指纹，两个服务器都尝试）
            keyservers = [
                'hkp://keys.openpgp.org',
                'hkp://keyserver.ubuntu.com'
            ]
            
            for fp in gpg_fingerprints:
                for keyserver in keyservers:
                    _recv_gpg_key_from_keyserver(fp, keyserver)
                
            # 删除等待消息
            try:
                bot.delete_message(message.chat.id, wait_msg.message_id)
            except Exception:
                pass
            
            # 再次尝试验证指纹
            success, signature_fingerprint, error_msg = _try_gpg_verify_fingerprint(
                temp_file, gpg_fingerprints
            )
            
            if success:
                _do_gpg_login_success(message.chat.id, asn)
                return
            
            # 第四步：所有自动方式都失败，询问用户是否手动上传公钥
            markup = ReplyKeyboardMarkup(resize_keyboard=True)
            markup.row_width = 1
            markup.add(
                KeyboardButton("📤 Upload Public Key 上传公钥"),
                KeyboardButton("❌ Cancel 取消")
            )
            
            msg = bot.send_message(
                message.chat.id,
                (
                    "⚠️ Could not verify the signature with available keys.\n"
                    "⚠️ 无法使用可用的密钥验证签名。\n"
                    "\n"
                    "Would you like to manually upload your GPG public key?\n"
                    "你想要手动上传你的 GPG 公钥吗？\n"
                    "\n"
                    "Note: The uploaded public key must match one of the fingerprints registered in the DN42 registry.\n"
                    "注意：上传的公钥必须与 DN42 registry 中注册的指纹之一匹配。"
                ),
                reply_markup=markup,
            )
            # 保存临时文件路径和相关信息，供后续验证使用
            bot.register_next_step_handler(
                msg, 
                partial(login_gpg_ask_manual_upload, asn, challenge, gpg_fingerprints, temp_file, signed_message)
            )
            # 不要在这里删除临时文件，后续还需要使用
            return
                
        except Exception as e:
            # 清理临时文件
            try:
                os.unlink(temp_file)
            except Exception:
                pass
            raise e
            
    except Exception as e:
        bot.send_message(
            message.chat.id,
            (
                "❌ Signature verification failed!\n"
                "❌ 签名验证失败！\n"
                "\n"
                f"Error: {str(e)}\n"
                f"错误: {str(e)}\n"
                "\n"
                "Please try to avoid signing in Windows command line, which may cause line ending issues.\n"
                "请尽量不要在 Windows 命令行中进行签名，这可能会导致换行符问题。\n"
                "\n"
                "You can use /login to retry.\n"
                "你可以使用 /login 重试。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )


def login_gpg_ask_manual_upload(asn, challenge, gpg_fingerprints, temp_file, signed_message, message):
    """询问用户是否手动上传公钥"""
    choice = message.text.strip()
    
    if choice == "/cancel" or "Cancel" in choice or "取消" in choice:
        # 清理临时文件
        try:
            os.unlink(temp_file)
        except Exception:
            pass
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    if "Upload" in choice or "上传" in choice:
        msg = bot.send_message(
            message.chat.id,
            (
                "📤 Please send your GPG public key.\n"
                "📤 请发送你的 GPG 公钥。\n"
                "\n"
                "You can:\n"
                "你可以：\n"
                "- Upload a `.asc` or `.txt` file containing the public key\n"
                "  上传包含公钥的 `.asc` 或 `.txt` 文件\n"
                "- Paste the public key directly (may require multiple messages)\n"
                "  直接粘贴公钥（可能需要多条消息）\n"
                "\n"
                "Export command / 导出命令：\n"
                "`gpg --armor --export <your-key-id>`\n"
                "\n"
                "Use /cancel to interrupt the operation.\n"
                "使用 /cancel 终止操作。"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        # 初始化空的公钥缓冲区，用于接收分段粘贴的公钥
        bot.register_next_step_handler(
            msg,
            partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, "")
        )
    else:
        # 无效选择，重新询问
        markup = ReplyKeyboardMarkup(resize_keyboard=True)
        markup.row_width = 1
        markup.add(
            KeyboardButton("📤 Upload Public Key 上传公钥"),
            KeyboardButton("❌ Cancel 取消")
        )
        msg = bot.send_message(
            message.chat.id,
            (
                "Invalid choice. Please select an option.\n"
                "无效的选择。请选择一个选项。"
            ),
            reply_markup=markup,
        )
        bot.register_next_step_handler(
            msg,
            partial(login_gpg_ask_manual_upload, asn, challenge, gpg_fingerprints, temp_file, signed_message)
        )


def login_gpg_receive_public_key(asn, challenge, gpg_fingerprints, temp_file, signed_message, key_buffer, message):
    """接收用户上传的公钥并验证
    
    支持：
    - 文件上传（.asc 或 .txt 文件）
    - 分段粘贴（等待收到完整的 PGP 公钥块）
    
    Args:
        key_buffer: 已接收的公钥内容缓冲区（用于分段粘贴）
    """
    # 检查是否取消
    if message.text and message.text.strip() == "/cancel":
        # 清理临时文件
        try:
            os.unlink(temp_file)
        except Exception:
            pass
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    public_key = None
    
    # 检查是否是文件上传
    if message.document:
        try:
            file_info = bot.get_file(message.document.file_id)
            downloaded_file = bot.download_file(file_info.file_path)
            public_key = downloaded_file.decode('utf-8')
        except Exception as e:
            msg = bot.send_message(
                message.chat.id,
                (
                    f"❌ Failed to read the uploaded file: {str(e)}\n"
                    f"❌ 读取上传文件失败: {str(e)}\n"
                    "\n"
                    "Please try again or use /cancel to abort.\n"
                    "请重试或使用 /cancel 取消。"
                ),
                reply_markup=ReplyKeyboardRemove(),
            )
            bot.register_next_step_handler(
                msg,
                partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, key_buffer)
            )
            return
    else:
        # 文本消息，追加到缓冲区
        text = message.text or ""
        # 每条 Telegram 消息末尾没有换行符，需要添加换行符来正确拼接
        if key_buffer and not key_buffer.endswith('\n'):
            key_buffer = key_buffer + "\n"
        key_buffer = key_buffer + text
        
        # 检查是否收到完整的公钥
        if "-----END PGP PUBLIC KEY BLOCK-----" in key_buffer:
            public_key = key_buffer
        elif "-----BEGIN PGP PUBLIC KEY BLOCK-----" in key_buffer:
            # 已经开始但还没结束，静默等待下一条消息
            bot.register_next_step_handler(
                message,
                partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, key_buffer)
            )
            return
        else:
            # 还没开始
            msg = bot.send_message(
                message.chat.id,
                (
                    "❌ Invalid GPG public key format.\n"
                    "❌ 无效的 GPG 公钥格式。\n"
                    "\n"
                    "The key should start with `-----BEGIN PGP PUBLIC KEY BLOCK-----`\n"
                    "公钥应该以 `-----BEGIN PGP PUBLIC KEY BLOCK-----` 开头\n"
                    "\n"
                    "Please try again or use /cancel to abort.\n"
                    "请重试或使用 /cancel 取消。"
                ),
                parse_mode="Markdown",
                reply_markup=ReplyKeyboardRemove(),
            )
            bot.register_next_step_handler(
                msg,
                partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, "")
            )
            return
    
    # 验证公钥格式
    if not public_key or "-----BEGIN PGP PUBLIC KEY BLOCK-----" not in public_key:
        msg = bot.send_message(
            message.chat.id,
            (
                "❌ Invalid GPG public key format.\n"
                "❌ 无效的 GPG 公钥格式。\n"
                "\n"
                "The key should contain `-----BEGIN PGP PUBLIC KEY BLOCK-----`\n"
                "公钥应该包含 `-----BEGIN PGP PUBLIC KEY BLOCK-----`\n"
                "\n"
                "Please try again or use /cancel to abort.\n"
                "请重试或使用 /cancel 取消。"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        bot.register_next_step_handler(
            msg,
            partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, "")
        )
        return
    
    try:
        # 导入公钥
        import_success, imported_fingerprint, import_error = _import_gpg_key_from_text(public_key)
        
        if not import_success:
            msg = bot.send_message(
                message.chat.id,
                (
                    "❌ Failed to import the public key.\n"
                    "❌ 导入公钥失败。\n"
                    "\n"
                    f"Error: {import_error}\n"
                    f"错误: {import_error}\n"
                    "\n"
                    "Please try again or use /cancel to abort.\n"
                    "请重试或使用 /cancel 取消。"
                ),
                reply_markup=ReplyKeyboardRemove(),
            )
            bot.register_next_step_handler(
                msg,
                partial(login_gpg_receive_public_key, asn, challenge, gpg_fingerprints, temp_file, signed_message, "")
            )
            return
        
        # 验证导入的公钥指纹是否与 registry 中的匹配
        fingerprints_upper = [fp.replace(' ', '').upper() for fp in gpg_fingerprints]
        
        fingerprint_matched = False
        if imported_fingerprint:
            fingerprint_matched = any(
                imported_fingerprint in fp or fp in imported_fingerprint
                for fp in fingerprints_upper
            )
        
        if not fingerprint_matched:
            # 清理临时文件
            try:
                os.unlink(temp_file)
            except Exception:
                pass
            
            bot.send_message(
                message.chat.id,
                (
                    "❌ The uploaded public key does not match any fingerprint registered in the DN42 registry.\n"
                    "❌ 上传的公钥与 DN42 registry 中注册的指纹不匹配。\n"
                    "\n"
                    f"Imported key fingerprint / 导入的密钥指纹: `{imported_fingerprint or 'unknown'}`\n"
                    f"Expected fingerprints / 期望的指纹:\n"
                    + "\n".join([f"- `{fp}`" for fp in gpg_fingerprints]) +
                    "\n\n"
                    "Please make sure you upload the correct public key.\n"
                    "请确保你上传了正确的公钥。\n"
                    "\n"
                    "You can use /login to retry.\n"
                    "你可以使用 /login 重试。"
                ),
                parse_mode="Markdown",
                reply_markup=ReplyKeyboardRemove(),
            )
            return
        
        # 公钥指纹匹配，重新验证签名
        success, signature_fingerprint, error_msg = _try_gpg_verify_fingerprint(
            temp_file, gpg_fingerprints
        )
        
        # 清理临时文件
        try:
            os.unlink(temp_file)
        except Exception:
            pass
        
        if success:
            _do_gpg_login_success(message.chat.id, asn)
        else:
            bot.send_message(
                message.chat.id,
                (
                    "❌ Signature verification still failed after importing the public key.\n"
                    "❌ 导入公钥后签名验证仍然失败。\n"
                    "\n"
                    f"Error: {error_msg}\n"
                    f"错误: {error_msg}\n"
                    "\n"
                    "You can use /login to retry.\n"
                    "你可以使用 /login 重试。"
                ),
                reply_markup=ReplyKeyboardRemove(),
            )
            
    except Exception as e:
        # 清理临时文件
        try:
            os.unlink(temp_file)
        except Exception:
            pass
        
        bot.send_message(
            message.chat.id,
            (
                "❌ An error occurred while processing the public key.\n"
                "❌ 处理公钥时发生错误。\n"
                "\n"
                f"Error: {str(e)}\n"
                f"错误: {str(e)}\n"
                "\n"
                "You can use /login to retry.\n"
                "你可以使用 /login 重试。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )


def login_signature_verify_ssh(asn, challenge, ssh_keys, message):
    """验证 SSH 签名
    
    Args:
        asn: AS号
        challenge: 挑战字符串
        ssh_keys: SSH 公钥列表
        message: 消息对象（包含用户发送的签名）
    """
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    
    signature = message.text.strip()
    
    # 验证签名
    try:
        import tempfile
        
        # 创建临时目录
        temp_dir = tempfile.mkdtemp()
        challenge_file = os.path.join(temp_dir, "challenge.txt")
        signature_file = os.path.join(temp_dir, "challenge.txt.sig")
        allowed_signers_file = os.path.join(temp_dir, "allowed_signers")
        
        try:
            # 写入挑战字符串（不带换行符，命令使用 echo -n）
            with open(challenge_file, 'w') as f:
                f.write(challenge)
            
            # 写入签名
            with open(signature_file, 'w') as f:
                f.write(signature)
            
            # 尝试每个 SSH 公钥进行验证
            verified = False
            for ssh_key in ssh_keys:
                # 创建 allowed_signers 文件
                # 格式: principal key-type key-data
                with open(allowed_signers_file, 'w') as f:
                    f.write(f"user@dn42 {ssh_key}\n")
                
                # 使用 ssh-keygen -Y verify 验证签名
                try:
                    result = subprocess.run(
                        [
                            'ssh-keygen', '-Y', 'verify',
                            '-f', allowed_signers_file,
                            '-I', 'user@dn42',
                            '-n', 'file',
                            '-s', signature_file
                        ],
                        stdin=open(challenge_file, 'r'),
                        capture_output=True,
                        text=True,
                        timeout=5
                    )
                    
                    # 检查返回码和输出（Good 可能在 stdout 或 stderr）
                    if result.returncode == 0 and ('Good' in result.stdout or 'Good' in result.stderr):
                        verified = True
                        break
                except Exception:
                    continue
            
            if verified:
                # 签名验证成功，执行登录
                db[message.chat.id] = asn
                data_dir = "./data"
                os.makedirs(data_dir, exist_ok=True)
                with open(os.path.join(data_dir, "user_db.pkl"), "wb") as f:
                    pickle.dump((db, db_privilege), f)
                
                bot.send_message(
                    message.chat.id,
                    (
                        f"✅ Signature verified successfully!\n"
                        f"✅ 签名验证成功！\n"
                        "\n"
                        f"Welcome! `{tools.get_asn_mnt_text(asn)}`\n"
                        f"欢迎你！`{tools.get_asn_mnt_text(asn)}`"
                    ),
                    parse_mode="Markdown",
                    reply_markup=ReplyKeyboardRemove(),
                )
            else:
                raise ValueError("SSH signature verification failed")
                
        finally:
            # 清理临时文件
            import shutil
            shutil.rmtree(temp_dir, ignore_errors=True)
            
    except Exception as e:
        bot.send_message(
            message.chat.id,
            (
                "❌ Signature verification failed!\n"
                "❌ 签名验证失败！\n"
                "\n"
                f"Error: {str(e)}\n"
                f"错误: {str(e)}\n"
                "\n"
                "Please try to avoid signing in Windows command line, which may cause line ending issues.\n"
                "请尽量不要在 Windows 命令行中进行签名，这可能会导致换行符问题。\n"
                "\n"
                "You can use /login to retry.\n"
                "你可以使用 /login 重试。"
            ),
            reply_markup=ReplyKeyboardRemove(),
        )


def login_choose_email(asn, emails, last_msg_id, message):
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    if (
        config.PRIVILEGE_CODE
        and (not (config.SINGLE_PRIVILEGE and db_privilege))
        and message.text.strip() == config.PRIVILEGE_CODE
    ):
        db[message.chat.id] = asn
        db_privilege.add(message.chat.id)
        data_dir = "./data"
        os.makedirs(data_dir, exist_ok=True)
        with open(os.path.join(data_dir, "user_db.pkl"), "wb") as f:
            pickle.dump((db, db_privilege), f)
        bot.delete_message(message.chat.id, message.message_id)
        bot.delete_message(message.chat.id, last_msg_id)
        bot.send_message(
            message.chat.id,
            (
                "*[Privilege]*\n"
                f"Welcome! `{tools.get_asn_mnt_text(asn)}`\n"
                f"欢迎你！`{tools.get_asn_mnt_text(asn)}`"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    if message.text.strip() not in emails:
        bot.send_message(
            message.chat.id,
            (
                "Sorry. For now, you can only use the email address you registered in the DN42 Registry to authenticate.\n"
                "抱歉。暂时只能使用您在 DN42 Registry 中登记的邮箱完成验证。\n"
                f"Please contact {config.CONTACT} for manual handling.\n"
                f"请联系 {config.CONTACT} 人工处理。"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    msg = bot.send_message(
        message.chat.id,
        (
            "Sending verification code...\n"
            "正在发送验证码...\n"
            "\n"
            "Hold on, this may take up to 2 minutes to send successfully.\n"
            "稍安勿躁，最多可能需要 2 分钟才能成功发送。"
        ),
        reply_markup=ReplyKeyboardRemove(),
    )
    bot.send_chat_action(chat_id=message.chat.id, action="typing")
    code = tools.gen_random_code(16)
    try:
        config.send_email(asn, tools.get_whoisinfo_by_asn(asn), code, message.text.strip())
    except RuntimeError:
        bot.delete_message(message.chat.id, msg.message_id)
        bot.send_message(
            message.chat.id,
            (
                "Sorry, we are unable to send the verification code to your email address at this time. Please try again later.\n"
                "抱歉，暂时无法发送验证码至您的邮箱。请稍后再试。\n"
                f"Please contact {config.CONTACT} if it keeps failing.\n"
                f"如果一直发送失败请联系 {config.CONTACT} 处理。"
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
    else:
        bot.delete_message(message.chat.id, msg.message_id)
        msg = bot.send_message(
            message.chat.id,
            (
                "Verification code has been sent to your email.\n"
                "验证码已发送至您的邮箱。\n"
                f"Please contact {config.CONTACT} if you can not receive it.\n"
                f"如果无法收到请联系 {config.CONTACT}\n"
                "\n"
                "Enter your verification code:\n"
                "请输入验证码："
            ),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
        bot.register_next_step_handler(msg, partial(login_verify_code, asn, code))


def login_verify_code(asn, code, message):
    if message.text.strip() == "/cancel":
        bot.send_message(
            message.chat.id,
            "Current operation has been cancelled.\n当前操作已被取消。",
            reply_markup=ReplyKeyboardRemove(),
        )
        return
    if message.text.strip().lower() == code.lower():
        db[message.chat.id] = asn
        data_dir = "./data"
        os.makedirs(data_dir, exist_ok=True)
        with open(os.path.join(data_dir, "user_db.pkl"), "wb") as f:
            pickle.dump((db, db_privilege), f)
        bot.send_message(
            message.chat.id,
            (f"Welcome! `{tools.get_asn_mnt_text(asn)}`\n" f"欢迎你！`{tools.get_asn_mnt_text(asn)}`"),
            parse_mode="Markdown",
            reply_markup=ReplyKeyboardRemove(),
        )
    else:
        bot.send_message(
            message.chat.id,
            ("Verification code error!\n" "验证码错误！\n" "You can use /login to retry.\n" "你可以使用 /login 重试。"),
            reply_markup=ReplyKeyboardRemove(),
        )
