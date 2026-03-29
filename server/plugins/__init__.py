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


def _clone_or_pull(git_url: str, name: str) -> tuple[str, bool]:
    """Clone repo if not present, otherwise git pull.

    Returns (local_path, updated) where updated=True if code changed.
    """
    dest = os.path.join(_REPOS_DIR, name)
    updated = False
    if os.path.isdir(os.path.join(dest, ".git")):
        # Capture current HEAD before pull
        old_head = subprocess.run(
            ["git", "-C", dest, "rev-parse", "HEAD"],
            capture_output=True, text=True, timeout=10,
        ).stdout.strip()

        print(f"[Plugin] Updating {name} ...")
        subprocess.run(
            ["git", "-C", dest, "pull", "--ff-only", "-q"],
            timeout=60,
            check=False,
        )

        new_head = subprocess.run(
            ["git", "-C", dest, "rev-parse", "HEAD"],
            capture_output=True, text=True, timeout=10,
        ).stdout.strip()

        updated = old_head != new_head
        if updated:
            print(f"[Plugin] {name} updated: {old_head[:8]} -> {new_head[:8]}")
    else:
        os.makedirs(_REPOS_DIR, exist_ok=True)
        print(f"[Plugin] Cloning {name} from {git_url} ...")
        subprocess.run(
            ["git", "clone", "--depth=1", "-q", git_url, dest],
            timeout=120,
            check=True,
        )
        updated = True  # Fresh clone always needs build
    return dest, updated


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


def _build_frontend(plugin_dir: str, name: str, force: bool = False):
    """Build web frontend if present and needed.

    Looks for web/frontend/package.json inside the plugin directory.
    Triggers a build when:
      - dist/ does not exist or is empty (first time)
      - force=True (plugin code was updated via git pull)
    Requires Node.js / npm to be available in the container.
    """
    frontend_dir = os.path.join(plugin_dir, "web", "frontend")
    pkg_json = os.path.join(frontend_dir, "package.json")
    dist_dir = os.path.join(frontend_dir, "dist")

    if not os.path.isfile(pkg_json):
        return  # No frontend to build

    need_build = False
    if not os.path.isdir(dist_dir) or not os.listdir(dist_dir):
        need_build = True
    elif force:
        print(f"[Plugin] Frontend source updated for {name}, rebuilding...")
        need_build = True

    if not need_build:
        print(f"[Plugin] Frontend already built for {name}, skipping.")
        return

    # Check npm availability
    npm_cmd = "npm"
    if subprocess.run(["which", "npm"], capture_output=True).returncode != 0:
        print(f"[Plugin] WARNING: npm not found, cannot build frontend for {name}.")
        print(f"[Plugin] Install Node.js in the Docker image or build frontend manually.")
        return

    print(f"[Plugin] Building frontend for {name} ...")
    try:
        subprocess.run(
            [npm_cmd, "install"],
            cwd=frontend_dir,
            timeout=120,
            check=True,
        )
        subprocess.run(
            [npm_cmd, "run", "build"],
            cwd=frontend_dir,
            timeout=120,
            check=True,
        )
        print(f"[Plugin] Frontend built successfully for {name}.")
    except subprocess.CalledProcessError as e:
        print(f"[Plugin] WARNING: Frontend build failed for {name}: {e}")
    except FileNotFoundError:
        print(f"[Plugin] WARNING: npm not found, skipping frontend build for {name}.")


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
            plugin_dir, updated = _clone_or_pull(git_url, name)

            # 2. Checkout specific branch if specified
            if branch:
                subprocess.run(
                    ["git", "-C", plugin_dir, "checkout", branch, "-q"],
                    timeout=30,
                    check=False,
                )

            # 3. Install deps
            _install_requirements(plugin_dir, name)

            # 3.5. Build web frontend if present (force rebuild if code updated)
            _build_frontend(plugin_dir, name, force=updated)

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
            # NOTE: Do NOT register under short name (sys.modules[name])
            # because it may shadow stdlib or third-party packages.
            # e.g. plugin name "email" would override Python's email module.
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
