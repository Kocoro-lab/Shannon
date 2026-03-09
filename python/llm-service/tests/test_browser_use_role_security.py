import asyncio
from unittest.mock import AsyncMock, patch

from llm_service.roles.presets import get_role_preset


def test_browser_use_role_does_not_advertise_evaluate():
    """Verify 'evaluate' is not exposed via role preset or tool schema."""
    from llm_service.tools.builtin.browser_use import BrowserTool, SAFE_ACTIONS

    preset = get_role_preset("browser_use")
    # Consolidated tool is in the allowlist
    assert "browser" in preset.get("allowed_tools", [])
    # evaluate is not in the advertised safe actions enum
    assert "evaluate" not in SAFE_ACTIONS
    # evaluate should not appear as a documented action in the prompt
    prompt = preset.get("system_prompt", "")
    assert 'action="evaluate"' not in prompt


def test_browser_evaluate_action_rejected_without_permission():
    """Verify evaluate action is rejected at runtime without explicit permission."""
    from llm_service.tools.builtin.browser_use import BrowserTool

    tool = BrowserTool()
    result = asyncio.get_event_loop().run_until_complete(
        tool._execute_impl(
            session_context={"session_id": "test-123"},
            action="evaluate",
            script="document.title",
        )
    )
    assert not result.success
    assert "permission" in result.error.lower()


def test_browser_evaluate_action_allowed_with_permission():
    """Verify evaluate action proceeds when allow_browser_evaluate is set."""
    from llm_service.tools.builtin.browser_use import BrowserTool

    tool = BrowserTool()

    with patch(
        "llm_service.tools.builtin.browser_use._call_playwright_action",
        new_callable=AsyncMock,
        return_value={"success": True, "result": "Test Page"},
    ):
        result = asyncio.get_event_loop().run_until_complete(
            tool._execute_impl(
                session_context={
                    "session_id": "test-123",
                    "allow_browser_evaluate": True,
                },
                action="evaluate",
                script="document.title",
            )
        )
    assert result.success
