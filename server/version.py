import os

REPOSITORY = "https://github.com/bingxin666/dn42-bot"
UPSTREAM = "https://github.com/Potat0000/dn42-bot"


def _read_file(path):
    try:
        with open(path) as f:
            return f.read().strip()
    except Exception:
        return None


def get_build_date():
    return _read_file("/app/build_date.txt") or "unknown"


def get_git_commit():
    return _read_file("/app/git_commit.txt") or "unknown"
