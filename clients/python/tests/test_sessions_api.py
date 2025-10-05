"""Lightweight checks for session API presence (no network)."""

from shannon import ShannonClient


def test_client_has_session_methods():
    c = ShannonClient()
    for name in [
        "create_session",
        "get_session",
        "update_session",
        "delete_session",
        "list_sessions",
        "add_message",
        "clear_history",
    ]:
        assert hasattr(c, name), f"Missing method: {name}"

