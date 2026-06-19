"""
DN42 Registry local clone management and query module.
Handles cloning, updating, and querying the DN42 registry from local files.
"""
import os
import subprocess
import threading
import time
from ipaddress import ip_address, ip_network, IPv4Address, IPv6Address
from pathlib import Path
from typing import Optional, Dict, List

# Registry configuration
REGISTRY_URL = "https://git.origami.pub/Bingxin/dn42-registry.git"
CACHE_DIR = "./cache"
REGISTRY_PATH = os.path.join(CACHE_DIR, "registry")

# Lock for git operations
_git_lock = threading.Lock()


class RegistryError(Exception):
    """Exception raised for registry-related errors."""
    pass


def ensure_registry_cloned() -> bool:
    """
    Ensure the DN42 registry is cloned to the cache directory.
    If not present, clone it. If present, try to update it.
    
    Returns:
        bool: True if registry is available, False otherwise
    """
    with _git_lock:
        # Create cache directory if it doesn't exist
        os.makedirs(CACHE_DIR, exist_ok=True)
        
        if os.path.exists(REGISTRY_PATH) and os.path.isdir(os.path.join(REGISTRY_PATH, ".git")):
            # Check if the remote URL matches the expected one; fix if outdated
            try:
                remote_result = subprocess.run(
                    ["git", "-C", REGISTRY_PATH, "remote", "get-url", "origin"],
                    capture_output=True,
                    timeout=10,
                    text=True
                )
                current_url = remote_result.stdout.strip() if remote_result.returncode == 0 else ""
                if current_url != REGISTRY_URL:
                    print(f"Registry remote URL mismatch: {current_url} -> {REGISTRY_URL}")
                    subprocess.run(
                        ["git", "-C", REGISTRY_PATH, "remote", "set-url", "origin", REGISTRY_URL],
                        capture_output=True,
                        timeout=10,
                        text=True
                    )
                    print("Registry remote URL updated successfully.")
            except (subprocess.TimeoutExpired, subprocess.SubprocessError) as e:
                print(f"Warning: failed to check/update remote URL: {e}")

            # Repository already exists, try to update it
            try:
                result = subprocess.run(
                    ["git", "-C", REGISTRY_PATH, "pull", "--ff-only"],
                    capture_output=True,
                    timeout=30,
                    text=True
                )
                if result.returncode == 0:
                    return True
                else:
                    print(f"Failed to update registry: {result.stderr}")
                    # If pull fails (e.g. history diverged after remote change), re-clone
                    print("Re-cloning registry due to update failure...")
                    import shutil
                    shutil.rmtree(REGISTRY_PATH)
                    clone_result = subprocess.run(
                        ["git", "clone", "--depth", "1", REGISTRY_URL, REGISTRY_PATH],
                        capture_output=True,
                        timeout=120,
                        text=True
                    )
                    if clone_result.returncode == 0:
                        print("Successfully re-cloned DN42 registry.")
                        return True
                    else:
                        print(f"Re-clone failed: {clone_result.stderr}")
                        return False
            except (subprocess.TimeoutExpired, subprocess.SubprocessError) as e:
                print(f"Error updating registry: {e}")
                # Continue to use existing repository
                return True
        else:
            # Repository doesn't exist, clone it
            try:
                # Remove any existing files in the path
                if os.path.exists(REGISTRY_PATH):
                    import shutil
                    shutil.rmtree(REGISTRY_PATH)
                
                # Try primary URL first
                result = subprocess.run(
                    ["git", "clone", "--depth", "1", REGISTRY_URL, REGISTRY_PATH],
                    capture_output=True,
                    timeout=120,
                    text=True
                )
                if result.returncode == 0:
                    print("Successfully cloned DN42 registry from primary source")
                    return True
                else:
                    print(f"Failed to clone from primary source: {result.stderr}")
                    return False
            except (subprocess.TimeoutExpired, subprocess.SubprocessError) as e:
                print(f"Error cloning registry: {e}")
                return False


def parse_registry_file(file_path: str) -> Dict[str, str]:
    """
    Parse a registry file and extract key-value pairs.
    
    Args:
        file_path: Path to the registry file
        
    Returns:
        Dict mapping keys to values
    """
    data = {}
    try:
        with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
            current_key = None
            for line in f:
                line = line.rstrip('\n')
                # Skip comments and empty lines
                if line.startswith('#') or line.startswith('%') or not line.strip():
                    continue
                
                # Check if this is a key-value line
                if ':' in line:
                    parts = line.split(':', 1)
                    key = parts[0].strip()
                    value = parts[1].strip() if len(parts) > 1 else ""
                    
                    # Handle multi-line values
                    if key:
                        current_key = key
                        if current_key not in data:
                            data[current_key] = []
                        if value:
                            data[current_key].append(value)
                elif current_key and line.startswith((' ', '\t')):
                    # Continuation line
                    data[current_key].append(line.strip())
    except (IOError, OSError) as e:
        print(f"Error reading file {file_path}: {e}")
    
    # Convert lists to strings (take first value for consistency with whois behavior)
    result = {}
    for key, values in data.items():
        if values:
            result[key] = values[0]  # Take first value
    
    return result


def find_asn_file(asn: int) -> Optional[str]:
    """
    Find the registry file for a given ASN.
    
    Args:
        asn: The ASN number
        
    Returns:
        Path to the file if found, None otherwise
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    
    # ASN files are stored in data/aut-num/
    asn_dir = os.path.join(REGISTRY_PATH, "data", "aut-num")
    if not os.path.exists(asn_dir):
        return None
    
    # ASN files are named like "AS4242420000" or "AS424242XXXX"
    asn_file = os.path.join(asn_dir, f"AS{asn}")
    if os.path.exists(asn_file):
        return asn_file
    
    return None


def find_person_file(person_id: str) -> Optional[str]:
    """
    Find the registry file for a given person/role.
    
    Args:
        person_id: The person or role identifier
        
    Returns:
        Path to the file if found, None otherwise
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    
    # Person files are stored in data/person/ and data/role/
    for subdir in ["person", "role"]:
        person_dir = os.path.join(REGISTRY_PATH, "data", subdir)
        if not os.path.exists(person_dir):
            continue
        
        person_file = os.path.join(person_dir, person_id)
        if os.path.exists(person_file):
            return person_file
    
    return None


def find_organisation_file(org_id: str) -> Optional[str]:
    """
    Find the registry file for a given organisation.
    
    Args:
        org_id: The organisation identifier
        
    Returns:
        Path to the file if found, None otherwise
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    
    # Organisation files are stored in data/organisation/
    org_dir = os.path.join(REGISTRY_PATH, "data", "organisation")
    if not os.path.exists(org_dir):
        return None
    
    org_file = os.path.join(org_dir, org_id)
    if os.path.exists(org_file):
        return org_file
    
    return None


def find_mntner_file(mntner_id: str) -> Optional[str]:
    """
    Find the registry file for a given maintainer.
    
    Args:
        mntner_id: The maintainer identifier
        
    Returns:
        Path to the file if found, None otherwise
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    
    # Maintainer files are stored in data/mntner/
    mntner_dir = os.path.join(REGISTRY_PATH, "data", "mntner")
    if not os.path.exists(mntner_dir):
        return None
    
    mntner_file = os.path.join(mntner_dir, mntner_id)
    if os.path.exists(mntner_file):
        return mntner_file
    
    return None


def _read_registry_file(file_path: str) -> Optional[str]:
    """Read and return the content of a registry file."""
    try:
        with open(file_path, 'r', encoding='utf-8', errors='ignore') as f:
            return f.read().strip()
    except (IOError, OSError) as e:
        print(f"Error reading registry file {file_path}: {e}")
        return None


def _find_file_in_dir(directory: str, filename: str) -> Optional[str]:
    """Find a file by exact name in a registry data subdirectory."""
    if not os.path.exists(REGISTRY_PATH):
        return None
    dir_path = os.path.join(REGISTRY_PATH, "data", directory)
    if not os.path.exists(dir_path):
        return None
    file_path = os.path.join(dir_path, filename)
    if os.path.exists(file_path):
        return file_path
    return None


def _find_ip_prefix_file(query: str, dirs: list) -> Optional[str]:
    """
    Find registry file for an IP address or CIDR prefix.
    Supports exact CIDR match and single IP lookup (most specific prefix).
    
    Args:
        query: IP address or CIDR prefix string
        dirs: list of (ipv4_dir, ipv6_dir) names to search
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    try:
        if '/' in query:
            net = ip_network(query, strict=False)
            filename = str(net.network_address) + "_" + str(net.prefixlen)
            subdir = dirs[1] if net.version == 6 else dirs[0]
            return _find_file_in_dir(subdir, filename)
        else:
            addr = ip_address(query)
    except ValueError:
        return None

    subdir = dirs[1] if isinstance(addr, IPv6Address) else dirs[0]
    dir_path = os.path.join(REGISTRY_PATH, "data", subdir)
    if not os.path.exists(dir_path):
        return None

    best_match = None
    best_prefixlen = -1
    try:
        for fname in os.listdir(dir_path):
            if '_' not in fname:
                continue
            try:
                net = ip_network(fname.replace('_', '/'), strict=False)
                if net.prefixlen == 0:
                    continue
                if addr in net and net.prefixlen > best_prefixlen:
                    best_prefixlen = net.prefixlen
                    best_match = os.path.join(dir_path, fname)
            except ValueError:
                continue
    except (IOError, OSError):
        pass
    return best_match


def get_whois_info_from_registry(query: str) -> Optional[str]:
    """
    Get whois information from local registry for a given query.
    
    Supports: ASN, inetnum/inet6num (IP/CIDR), route/route6,
    person, role, mntner, organisation, dns, as-set, as-block,
    key-cert, route-set, schema.
    
    Args:
        query: ASN, IP/CIDR, domain, or other identifier
        
    Returns:
        Full whois text from the registry file, or None if not found
    """
    if not os.path.exists(REGISTRY_PATH):
        return None
    
    # Try to parse as ASN. Avoid Python's permissive int parsing here so
    # special whois queries like +04243517 pass through to remote whois.
    asn_str = query.upper()
    if asn_str.startswith("AS"):
        asn_str = asn_str[2:]
    
    try:
        if asn_str.isdigit():
            asn = int(asn_str)
            file_path = find_asn_file(asn)
            if file_path:
                return _read_registry_file(file_path)
    except ValueError:
        pass
    
    # Try as IP address or CIDR (inetnum/inet6num + route/route6)
    inetnum_path = _find_ip_prefix_file(query, ["inetnum", "inet6num"])
    if inetnum_path:
        content = _read_registry_file(inetnum_path)
        if content:
            route_path = _find_ip_prefix_file(query, ["route", "route6"])
            if route_path:
                route_content = _read_registry_file(route_path)
                if route_content:
                    content += f"\n\n{route_content}"
            return content

    route_path = _find_ip_prefix_file(query, ["route", "route6"])
    if route_path:
        return _read_registry_file(route_path)
    
    # Try as person/role
    file_path = find_person_file(query)
    if file_path:
        return _read_registry_file(file_path)
    
    # Try as maintainer
    file_path = find_mntner_file(query)
    if file_path:
        return _read_registry_file(file_path)
    
    # Try as organisation
    file_path = find_organisation_file(query)
    if file_path:
        return _read_registry_file(file_path)
    
    # Try remaining types: dns, as-set, as-block, key-cert, route-set, schema
    for subdir in ["dns", "as-set", "as-block", "key-cert", "route-set", "schema"]:
        file_path = _find_file_in_dir(subdir, query)
        if file_path:
            return _read_registry_file(file_path)
    
    return None


def get_asn_field(asn: int, field: str) -> Optional[str]:
    """
    Get a specific field value for an ASN from the local registry.
    
    Args:
        asn: The ASN number
        field: The field name (e.g., "mnt-by", "admin-c", "as-name")
        
    Returns:
        Field value if found, None otherwise
    """
    file_path = find_asn_file(asn)
    if not file_path:
        return None
    
    data = parse_registry_file(file_path)
    return data.get(field)


def list_all_asns() -> List[int]:
    """
    List all ASNs in the registry.
    
    Returns:
        List of ASN numbers
    """
    asns = []
    asn_dir = os.path.join(REGISTRY_PATH, "data", "aut-num")
    
    if not os.path.exists(asn_dir):
        return asns
    
    try:
        for filename in os.listdir(asn_dir):
            if filename.startswith("AS"):
                try:
                    asn = int(filename[2:])
                    asns.append(asn)
                except ValueError:
                    continue
    except (IOError, OSError) as e:
        print(f"Error listing ASNs: {e}")
    
    return sorted(asns)


# Registry initialization state
_init_complete = threading.Event()
_init_success = False


# Initialize registry on module load (in background to avoid blocking)
def _init_registry():
    """Initialize registry in background thread."""
    global _init_success
    try:
        _init_success = ensure_registry_cloned()
    except Exception as e:
        print(f"Failed to initialize registry: {e}")
        _init_success = False
    finally:
        _init_complete.set()


def wait_for_init(timeout=30):
    """
    Wait for registry initialization to complete.
    
    Args:
        timeout: Maximum time to wait in seconds
        
    Returns:
        bool: True if initialization succeeded, False otherwise
    """
    _init_complete.wait(timeout=timeout)
    return _init_success


# Start initialization in background
_init_thread = threading.Thread(target=_init_registry, daemon=True)
_init_thread.start()
