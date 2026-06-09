"""
Pickle-based peer registry for tracking peers across nodes.

Peers are keyed by (node_id, remote_asn) tuples and stored in ./data/peer_registry.pkl.
"""

import os
import pickle

_REGISTRY_PATH = os.path.join("./data", "peer_registry.pkl")

_registry: dict[tuple[str, int], dict] | None = None


def load_registry() -> dict[tuple[str, int], dict]:
    """Load the peer registry from disk.

    Returns:
        dict mapping (node_id, remote_asn) -> peer_info dict.
        Returns empty dict if no registry file exists.
    """
    global _registry
    if _registry is not None:
        return _registry
    try:
        os.makedirs("./data", exist_ok=True)
        with open(_REGISTRY_PATH, "rb") as f:
            _registry = pickle.load(f)
    except BaseException:
        _registry = {}
    return _registry


def save_registry() -> None:
    """Persist the current in-memory registry to disk."""
    global _registry
    if _registry is None:
        _registry = {}
    os.makedirs("./data", exist_ok=True)
    with open(_REGISTRY_PATH, "wb") as f:
        pickle.dump(_registry, f)


def _invalidate_cache() -> None:
    """Mark the in-memory registry as needing a reload from disk."""
    global _registry
    _registry = None


def add_peer(node_id: str, asn: int, info: dict) -> None:
    """Add or update a peer in the registry.

    Args:
        node_id: Target node identifier.
        asn: Peer's remote ASN.
        info: Peer info dict. Must include at least the fields from
              the peer-import-export schema (remote_pubkey, remote_endpoint, etc.).
    """
    registry = load_registry()
    merged = {"node_id": node_id, "remote_asn": asn, **info}
    registry[(node_id, asn)] = merged
    save_registry()


def remove_peer(node_id: str, asn: int) -> bool:
    """Remove a peer from the registry.

    Args:
        node_id: Target node identifier.
        asn: Peer's remote ASN.

    Returns:
        True if the peer was found and removed, False otherwise.
    """
    registry = load_registry()
    key = (node_id, asn)
    if key in registry:
        del registry[key]
        save_registry()
        return True
    return False


def get_peer(node_id: str, asn: int) -> dict | None:
    """Retrieve a single peer by node_id and ASN.

    Args:
        node_id: Target node identifier.
        asn: Peer's remote ASN.

    Returns:
        Peer info dict, or None if not found.
    """
    registry = load_registry()
    return registry.get((node_id, asn))


def get_peers_by_node(node_id: str) -> list[dict]:
    """Return all peers associated with a given node.

    Args:
        node_id: Target node identifier.

    Returns:
        List of peer info dicts for that node.
    """
    registry = load_registry()
    return [peer for (nid, _), peer in registry.items() if nid == node_id]


def get_peers_by_asn(asn: int) -> list[dict]:
    """Return all peers associated with a given remote ASN.

    Args:
        asn: Remote ASN to search for.

    Returns:
        List of peer info dicts matching that ASN.
    """
    registry = load_registry()
    return [peer for (_, a), peer in registry.items() if a == asn]
