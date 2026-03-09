"""Tests for _parse_history_entries — session history deserialization."""

import pytest

from llm_service.api.agent import _parse_history_entries


class TestParseHistoryEntries:
    """Test the shared history parser that handles list, dict, and legacy string formats."""

    # --- New format: list of strings (from Rust JSON array) ---

    def test_list_of_strings_basic(self):
        """Standard new-format: list of 'role: content' strings."""
        raw = ["user: hello", "assistant: hi there"]
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0] == {"role": "user", "content": "hello"}
        assert result[1] == {"role": "assistant", "content": "hi there"}

    def test_list_of_strings_with_escaped_newlines(self):
        """Content with escaped newlines should be unescaped."""
        raw = [r"assistant: Line one\nLine two\nLine three"]
        result = _parse_history_entries(raw)
        assert len(result) == 1
        assert result[0]["content"] == "Line one\nLine two\nLine three"

    def test_list_of_strings_real_newline_escaped_by_go(self):
        """Go escapes real newlines as backslash+n. Python should restore them."""
        # Go: strings.ReplaceAll(content, "\n", "\\n") produces literal \n (2 chars)
        # JSON round-trip preserves this. Python receives backslash+n.
        raw = ["assistant: Line one\\nLine two\\nLine three"]
        result = _parse_history_entries(raw)
        assert len(result) == 1
        assert result[0]["content"] == "Line one\nLine two\nLine three"

    def test_list_of_strings_empty_entries_skipped(self):
        raw = ["user: hello", "", "  ", "assistant: world"]
        result = _parse_history_entries(raw)
        assert len(result) == 2

    def test_list_of_strings_invalid_role_skipped(self):
        raw = ["user: hello", "system: you are helpful", "assistant: hi"]
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0]["role"] == "user"
        assert result[1]["role"] == "assistant"

    # --- New format: list of dicts (future-proof) ---

    def test_list_of_dicts(self):
        raw = [
            {"role": "user", "content": "hello"},
            {"role": "assistant", "content": "hi there"},
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0] == {"role": "user", "content": "hello"}
        assert result[1] == {"role": "assistant", "content": "hi there"}

    def test_list_of_dicts_with_newlines_in_content(self):
        raw = [
            {"role": "assistant", "content": "Line one\nLine two"},
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 1
        # Dict content should preserve newlines as-is (no double escaping)
        assert result[0]["content"] == "Line one\nLine two"

    def test_list_of_dicts_invalid_role_skipped(self):
        raw = [
            {"role": "system", "content": "ignored"},
            {"role": "user", "content": "hello"},
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 1
        assert result[0]["role"] == "user"

    def test_mixed_list_strings_and_dicts(self):
        raw = [
            "user: hello",
            {"role": "assistant", "content": "hi"},
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 2

    # --- Legacy format: newline-delimited string ---

    def test_legacy_string_basic(self):
        raw = "user: hello\nassistant: hi there"
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0] == {"role": "user", "content": "hello"}
        assert result[1] == {"role": "assistant", "content": "hi there"}

    def test_legacy_string_multiline_content_accumulated(self):
        """Multi-line assistant content should be accumulated, not truncated."""
        raw = "user: help\nassistant: Line one\nLine two\nLine three\nuser: thanks"
        result = _parse_history_entries(raw)
        assert len(result) == 3
        assert result[0] == {"role": "user", "content": "help"}
        assert result[1] == {"role": "assistant", "content": "Line one\nLine two\nLine three"}
        assert result[2] == {"role": "user", "content": "thanks"}

    def test_legacy_string_with_escaped_newlines(self):
        """Legacy format uses real newlines as delimiters. Escaped \\n within entries is unescaped."""
        # In legacy format, real \n separates turns. Escaped \\n is within content.
        raw = "user: hello\nassistant: Line one\\nLine two"
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0] == {"role": "user", "content": "hello"}
        assert result[1]["content"] == "Line one\nLine two"

    def test_legacy_string_empty(self):
        result = _parse_history_entries("")
        assert result == []

    def test_legacy_string_whitespace(self):
        result = _parse_history_entries("   \n\n  ")
        assert result == []

    # --- Edge cases ---

    def test_none_input(self):
        result = _parse_history_entries(None)
        assert result == []

    def test_numeric_input(self):
        result = _parse_history_entries(12345)
        assert result == []

    def test_empty_list(self):
        result = _parse_history_entries([])
        assert result == []

    def test_japanese_content(self):
        raw = [
            "user: 平日な朝6時にUPWARDの競合の広告を確認したい",
            r"assistant: ご質問ありがとうございます。\n\nキーワードを教えてください。",
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert "UPWARD" in result[0]["content"]
        assert "キーワード" in result[1]["content"]
        # Verify newline was unescaped
        assert "\n" in result[1]["content"]

    def test_colon_in_content_preserved(self):
        """Content containing ': ' should not break parsing."""
        raw = ["user: time is 10:30", "assistant: URL: https://example.com"]
        result = _parse_history_entries(raw)
        assert len(result) == 2
        assert result[0]["content"] == "time is 10:30"
        assert result[1]["content"] == "URL: https://example.com"

    def test_full_structured_conversation_round_trip(self):
        """Simulate a real multi-turn structured JSON conversation."""
        raw = [
            "user: 平日な朝6時にUPWARDの競合になりうるサイトの広告のクリエティブを確認したい",
            r'assistant: {"type":"clarification","message":"キーワードを教えてください"}',
            "user: 営業支援やCRM、SFA",
            r'assistant: {"type":"clarification","message":"朝6時と朝9時のどちらですか？"}',
            "user: 朝6時",
        ]
        result = _parse_history_entries(raw)
        assert len(result) == 5
        # Verify all messages are present with correct roles
        assert result[0]["role"] == "user"
        assert result[1]["role"] == "assistant"
        assert result[2]["role"] == "user"
        assert result[3]["role"] == "assistant"
        assert result[4]["role"] == "user"
        # Verify content is complete
        assert "UPWARD" in result[0]["content"]
        assert "clarification" in result[1]["content"]
        assert "朝6時" in result[4]["content"]
