"""
dn42-bot Plugin System (Git-based)
插件系统（基于 Git 仓库）

Plugins are loaded from external Git repositories specified in config.PLUGINS.
Each entry is a dict:  {"git": "<repo_url>", "name": "<plugin_name>"}

At startup the loader will:
  1. Clone (or pull) each repo into  ./data/plugins_repos/<name>/
  2. Install requirements.txt if present  (pip install -r ...)
  3. Import the directory as a Python package and call register()

插件从 config.PLUGINS 指定的外部 Git 仓库加载。
启动时会自动 clone / pull 到 ./data/plugins_repos/<name>/，
安装依赖，然后动态导入并注册。
"""

import importlib
import os
import subprocess
import sys
import traceback

_loaded_plugins = {}

# Where plugin repos are cloned to (relative to server CWD)
_REPOS_DIR = os.path.join(".", "data", "plugins_repos")


def _clone_or_pull(git_url: str, name: str) -> str:
    """Clone repo if not present, otherwise git pull. Returns local path."""
    dest = os.path.join(_REPOS_DIR, name)
    if os.path.isdir(os.path.join(dest, ".git")):
        print(f"[Plugin] Updating {name} ...")
        subprocess.run(
            ["git", "-C", dest, "pull", "--ff-only", "-q"],
            timeout=60,
            check=False,
        )
    else:
        os.makedirs(_REPOS_DIR, exist_ok=True)
        print(f"[Plugin] Cloning {name} from {git_url} ...")
        subprocess.run(
            ["git", "clone", "--depth=1", "-q", git_url, dest],
            timeout=120,
            check=True,
        )
    return dest


def _install_requirements(plugin_dir: str, name: str):
    """Install requirements.txt if present."""
    req_file = os.path.join(plugin_dir, "requirements.txt")
    if os.path.isfile(req_file):
        print(f"[Plugin] Installing dependencies for {name} ...")
        subprocess.run(
            [sys.executable, "-m", "pip", "install", "-q", "-r", req_file],
            timeout=120,
            check=True,
        )


def load_plugins():
    """Load all plugins defined in config.PLUGINS.

    加载 config.PLUGINS 中定义的所有插件。
    """
    try:
        import config
        plugin_list = getattr(config, "PLUGINS", None)
    except ImportError:
        plugin_list = None

    if not plugin_list:
        return

    for entry in plugin_list:
        git_url = entry.get("git", "")
        name = entry.get("name", "")
        branch = entry.get("branch", None)
        if not git_url or not name:
            print(f"[Plugin] Skipped invalid entry: {entry}")
            continue

        try:
            # 1. Clone / pull
            plugin_dir = _clone_or_pull(git_url, name)

            # 2. Checkout specific branch if specified
            if branch:
                subprocess.run(
                    ["git", "-C", plugin_dir, "checkout", branch, "-q"],
                    timeout=30,
                    check=False,
                )

            # 3. Install deps
            _install_requirements(plugin_dir, name)

            # 4. Import as package using spec_from_file_location
            #    so the plugin's relative imports (from . import xxx) work.
            repos_abs = os.path.abspath(_REPOS_DIR)
            if repos_abs not in sys.path:
                sys.path.insert(0, repos_abs)

            module_name = f"_dn42_plugin_{name}"
            spec = importlib.util.spec_from_file_location(
                module_name,
                os.path.join(plugin_dir, "__init__.py"),
                submodule_search_locations=[os.path.abspath(plugin_dir)],
            )
            if spec is None or spec.loader is None:
                print(f"[Plugin] Failed to find __init__.py for {name}")
                continue

            module = importlib.util.module_from_spec(spec)
            sys.modules[module_name] = module
            # Register under short name so relative imports resolve
            sys.modules[name] = module
            spec.loader.exec_module(module)

            # 5. Register handlers
            if hasattr(module, "register"):
                module.register()

            _loaded_plugins[name] = module
            print(f"[Plugin] Loaded: {name}")

        except Exception:
            print(f"[Plugin] Failed to load: {name}")
            traceback.print_exc()


def get_loaded_plugins():
    """Return a dict of loaded plugin name -> module."""
    return dict(_loaded_plugins)
