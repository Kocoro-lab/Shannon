"""Tests for strip_markdown_json_wrapper robustness."""

from llm_service.api.agent import strip_markdown_json_wrapper


class TestStripMarkdownJsonWrapper:
    def test_clean_json_no_fences(self):
        text = '{"action": "idle", "response": "done"}'
        assert strip_markdown_json_wrapper(text, expect_json=True) == text

    def test_clean_fenced_json(self):
        text = '```json\n{"action": "tool_call", "tool": "web_search"}\n```'
        result = strip_markdown_json_wrapper(text, expect_json=True)
        assert result == '{"action": "tool_call", "tool": "web_search"}'

    def test_fenced_json_with_trailing_text(self):
        """The exact bug: LLM outputs JSON in fences + notes after."""
        text = (
            '```json\n'
            '{"action": "tool_call", "tool": "web_fetch", "tool_params": {"urls": ["https://example.com"]}}\n'
            '```\n\n'
            '**notes:**\n'
            '- Already have: M365 pricing tiers\n'
            '- Need: Detailed Teams-only features'
        )
        result = strip_markdown_json_wrapper(text, expect_json=True)
        assert '"action": "tool_call"' in result
        assert "notes" not in result
        assert result.startswith("{")

    def test_fenced_json_no_lang_tag(self):
        text = '```\n{"action": "idle"}\n```'
        result = strip_markdown_json_wrapper(text, expect_json=True)
        assert result == '{"action": "idle"}'

    def test_non_json_fence_preserved(self):
        text = '```python\nprint("hello")\n```'
        assert strip_markdown_json_wrapper(text, expect_json=True) == text

    def test_expect_json_false_preserves(self):
        text = '```json\n{"a": 1}\n```'
        assert strip_markdown_json_wrapper(text, expect_json=False) == text

    def test_no_closing_fence_preserved(self):
        text = '```json\n{"action": "idle"}\nsome trailing text without fence'
        assert strip_markdown_json_wrapper(text, expect_json=True) == text

    def test_empty_and_none(self):
        assert strip_markdown_json_wrapper("", expect_json=True) == ""
        assert strip_markdown_json_wrapper(None, expect_json=True) is None

    def test_array_in_fences(self):
        text = '```json\n[1, 2, 3]\n```'
        result = strip_markdown_json_wrapper(text, expect_json=True)
        assert result == "[1, 2, 3]"
