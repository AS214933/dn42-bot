#!/usr/bin/env python3
"""
Scan the local DN42 registry clone and extract all contact platform patterns.

Outputs:
  1. Platform frequency table (how many records mention each platform)
  2. Sample entries per platform for regex development
  3. Summary statistics

Usage:
    python3 scripts/scan_contact_platforms.py [--registry-path ./cache/dn42-registry]
"""
import os
import re
import sys
import argparse
from collections import defaultdict, Counter
from pathlib import Path


# Platform definitions: (canonical_name, keywords_in_contact_key, regex_for_remarks)
PLATFORM_DEFS = {
    "telegram": {
        "contact_keys": {"telegram", "tg", "telegra"},
        "remarks_pattern": re.compile(
            r"(?:https?://)?t\.me/([A-Za-z0-9_]{2,})|"
            r"telegram[^A-Za-z0-9_@]*@?([A-Za-z0-9_]{2,})|"
            r"(?<![A-Za-z0-9._%+-])@([A-Za-z0-9_]{2,}).*telegram",
            re.IGNORECASE,
        ),
    },
    "discord": {
        "contact_keys": {"discord"},
        "remarks_pattern": re.compile(
            r"discord[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_.-]{2,}(?:#[0-9]+)?)|"
            r"discord\.gg/([A-Za-z0-9_-]+)",
            re.IGNORECASE,
        ),
    },
    "irc": {
        "contact_keys": {"irc", "ircs", "hackint"},
        "remarks_pattern": re.compile(
            r"\birc\b[^A-Za-z0-9]{0,10}(?:\([^)]*\)\s*)?[^A-Za-z0-9]{0,5}[:>-]",
            re.IGNORECASE,
        ),
    },
    "matrix": {
        "contact_keys": {"matrix"},
        "remarks_pattern": re.compile(
            r"matrix[^A-Za-z0-9]{0,10}[:>-]?\s*(@[^:\s]+:[^\s,;]+)|"
            r"(#[^\s:]+:[a-zA-Z0-9.-]+\.[a-z]{2,})",
            re.IGNORECASE,
        ),
    },
    "xmpp": {
        "contact_keys": {"xmpp", "jabber"},
        "remarks_pattern": re.compile(
            r"(?:xmpp|jabber)[^A-Za-z0-9]{0,10}[:>-]?\s*([^\s,;]+@[^\s,;]+)",
            re.IGNORECASE,
        ),
    },
    "signal": {
        "contact_keys": {"signal"},
        "remarks_pattern": re.compile(
            r"signal[^A-Za-z0-9]{0,10}[:>-]?\s*([A-Za-z0-9_.-]{2,})",
            re.IGNORECASE,
        ),
    },
    "twitter": {
        "contact_keys": {"twitter"},
        "remarks_pattern": re.compile(
            r"(?:twitter|x\.com)[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_]{1,15})",
            re.IGNORECASE,
        ),
    },
    "mastodon": {
        "contact_keys": {"mastodon", "fediverse", "fedi", "activitypub"},
        "remarks_pattern": re.compile(
            r"(?:mastodon|fediverse|fedi|activitypub)[^A-Za-z0-9]{0,10}[:>-]?\s*@?([^\s,;]+@[^\s,;]+)",
            re.IGNORECASE,
        ),
    },
    "github": {
        "contact_keys": {"github"},
        "remarks_pattern": re.compile(
            r"github[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_-]{1,39})",
            re.IGNORECASE,
        ),
    },
    "gitlab": {
        "contact_keys": {"gitlab"},
        "remarks_pattern": re.compile(
            r"gitlab[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_-]{1,39})",
            re.IGNORECASE,
        ),
    },
    "keybase": {
        "contact_keys": {"keybase"},
        "remarks_pattern": re.compile(
            r"keybase[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_]{1,39})",
            re.IGNORECASE,
        ),
    },
    "keyoxide": {
        "contact_keys": {"keyoxide"},
        "remarks_pattern": re.compile(
            r"keyoxide[^A-Za-z0-9]{0,10}[:>-]?\s*(https?://[^\s]+|[A-Fa-f0-9]{40})",
            re.IGNORECASE,
        ),
    },
    "line": {
        "contact_keys": {"line"},
        "remarks_pattern": re.compile(
            r"\bline\b[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_.-]{2,})",
            re.IGNORECASE,
        ),
    },
    "qq": {
        "contact_keys": {"qq"},
        "remarks_pattern": re.compile(
            r"\bqq\b[^A-Za-z0-9]{0,10}[:>-]?\s*([0-9]{5,12})",
            re.IGNORECASE,
        ),
    },
    "wechat": {
        "contact_keys": {"wechat"},
        "remarks_pattern": re.compile(
            r"wechat[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_-]{2,})",
            re.IGNORECASE,
        ),
    },
    "whatsapp": {
        "contact_keys": {"whatsapp"},
        "remarks_pattern": re.compile(
            r"whatsapp[^A-Za-z0-9]{0,10}[:>-]?\s*\+?([0-9 -]{7,})",
            re.IGNORECASE,
        ),
    },
    "wire": {
        "contact_keys": {"wire"},
        "remarks_pattern": re.compile(
            r"\bwire\b[^A-Za-z0-9]{0,10}[:>-]?\s*@?([A-Za-z0-9_.-]{2,})",
            re.IGNORECASE,
        ),
    },
    "tox": {
        "contact_keys": {"tox"},
        "remarks_pattern": re.compile(
            r"\btox\b[^A-Za-z0-9]{0,10}[:>-]?\s*([A-Fa-f0-9]{76})",
            re.IGNORECASE,
        ),
    },
    "jami": {
        "contact_keys": {"jami"},
        "remarks_pattern": re.compile(
            r"\bjami\b[^A-Za-z0-9]{0,10}[:>-]?\s*([A-Fa-f0-9]{40,})",
            re.IGNORECASE,
        ),
    },
}


def parse_args():
    parser = argparse.ArgumentParser(description="Scan DN42 registry for contact platform patterns")
    parser.add_argument(
        "--registry-path",
        default="./cache/dn42-registry",
        help="Path to the DN42 registry clone (default: ./cache/dn42-registry)",
    )
    parser.add_argument(
        "--samples",
        type=int,
        default=5,
        help="Number of sample entries to show per platform (default: 5)",
    )
    return parser.parse_args()


def scan_person_role_files(registry_path, subdir):
    """Scan person/role files and yield (filename, lines) tuples."""
    dir_path = os.path.join(registry_path, "data", subdir)
    if not os.path.exists(dir_path):
        return
    for fname in os.listdir(dir_path):
        fpath = os.path.join(dir_path, fname)
        if os.path.isfile(fpath):
            try:
                with open(fpath, "r", encoding="utf-8", errors="ignore") as f:
                    lines = f.readlines()
                yield fname, lines
            except (IOError, OSError):
                continue


def scan_all_records(registry_path):
    """Scan all relevant data directories and yield (source, filename, lines)."""
    for subdir in ["person", "role", "aut-num", "organisation", "mntner"]:
        for fname, lines in scan_person_role_files(registry_path, subdir):
            yield subdir, fname, lines


def extract_contact_entries(lines):
    """
    Parse lines and extract structured contact entries.
    Returns list of (key, value, line_text) tuples.
    """
    entries = []
    current_key = None
    for line in lines:
        stripped = line.rstrip("\n")
        if not stripped or stripped.startswith("#") or stripped.startswith("%"):
            continue
        if ":" in stripped and not stripped.startswith((" ", "\t")):
            key, _, value = stripped.partition(":")
            key = key.strip().lower()
            value = value.strip()
            current_key = key
            if key and value:
                entries.append((key, value, stripped))
        elif current_key and stripped.startswith((" ", "\t")):
            value = stripped.strip()
            if value:
                entries.append((current_key, value, stripped))
    return entries


def main():
    args = parse_args()
    registry_path = args.registry_path

    if not os.path.exists(registry_path):
        print(f"Error: Registry path not found: {registry_path}", file=sys.stderr)
        sys.exit(1)

    # Statistics
    platform_counts = Counter()           # platform -> number of records containing it
    platform_samples = defaultdict(list)   # platform -> list of (source, filename, value)
    platform_contact_counts = Counter()    # platform -> number of contact: key hits
    total_records = 0
    records_with_contact = 0

    for source, fname, lines in scan_all_records(registry_path):
        total_records += 1
        entries = extract_contact_entries(lines)
        record_has_contact = False

        # Track platforms found in this record
        record_platforms = set()

        for key, value, raw_line in entries:
            # 1. Check contact: <platform>: entries
            if key == "contact":
                # Try to extract platform prefix from value
                # Format: "telegram: @handle" or "xmpp:user@domain"
                for platform, pdef in PLATFORM_DEFS.items():
                    for ck in pdef["contact_keys"]:
                        # Match "platform: value" or "platform:value"
                        pattern = re.compile(
                            rf"^{re.escape(ck)}\s*[:>-]\s*(.+)$", re.IGNORECASE
                        )
                        m = pattern.match(value)
                        if m:
                            record_platforms.add(platform)
                            platform_contact_counts[platform] += 1
                            if len(platform_samples[platform]) < args.samples:
                                platform_samples[platform].append(
                                    (source, fname, raw_line)
                                )
                            record_has_contact = True
                            break

            # 2. Check email keys
            if key in ("e-mail", "abuse-mailbox", "mail", "mailto", "email"):
                if re.search(r"@.+\.", value):
                    record_platforms.add("email")
                    platform_contact_counts["email"] += 1
                    if len(platform_samples["email"]) < args.samples:
                        platform_samples["email"].append(
                            (source, fname, raw_line)
                        )
                    record_has_contact = True

            # 3. Check phone keys
            if key in ("phone", "telephone", "tel", "fax-no", "mobile"):
                record_platforms.add("phone")
                platform_contact_counts["phone"] += 1
                if len(platform_samples["phone"]) < args.samples:
                    platform_samples["phone"].append(
                        (source, fname, raw_line)
                    )
                record_has_contact = True

            # 4. Scan remarks/descr/contact values for platform patterns
            if key in ("remarks", "descr", "contact"):
                for platform, pdef in PLATFORM_DEFS.items():
                    if pdef["remarks_pattern"].search(value):
                        record_platforms.add(platform)
                        if len(platform_samples[platform]) < args.samples:
                            platform_samples[platform].append(
                                (source, fname, raw_line)
                            )

        for p in record_platforms:
            platform_counts[p] += 1
        if record_has_contact:
            records_with_contact += 1

    # Print results
    print("=" * 72)
    print("DN42 Registry Contact Platform Analysis")
    print("=" * 72)
    print(f"Registry path: {os.path.abspath(registry_path)}")
    print(f"Total records scanned: {total_records}")
    print(f"Records with structured contact info: {records_with_contact}")
    print()

    print("-" * 72)
    print(f"{'Platform':<16} {'Records':>8} {'contact: hits':>14}  Sample")
    print("-" * 72)
    for platform, count in platform_counts.most_common():
        cc = platform_contact_counts.get(platform, 0)
        sample = ""
        if platform_samples[platform]:
            _, fname, val = platform_samples[platform][0]
            sample = val.strip()[:44]
        print(f"{platform:<16} {count:>8} {cc:>14}  {sample}")
    print("-" * 72)

    # Print detailed samples per platform
    print()
    print("=" * 72)
    print("Detailed Samples Per Platform")
    print("=" * 72)
    for platform in sorted(platform_samples.keys()):
        samples = platform_samples[platform]
        if not samples:
            continue
        print(f"\n--- {platform} ({len(samples)} samples) ---")
        for source, fname, raw_line in samples:
            print(f"  [{source}/{fname}] {raw_line.strip()}")


if __name__ == "__main__":
    main()
