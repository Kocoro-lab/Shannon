"""Tests for structured output (output_config) support."""

import pytest
from llm_provider.base import CompletionRequest, ModelTier


class TestCompletionRequestOutputConfig:
    """Test output_config field on CompletionRequest."""

    def test_output_config_default_none(self):
        req = CompletionRequest(messages=[{"role": "user", "content": "hi"}])
        assert req.output_config is None

    def test_output_config_accepts_dict(self):
        schema = {
            "format": {
                "type": "json_schema",
                "schema": {
                    "type": "object",
                    "properties": {"name": {"type": "string"}},
                    "required": ["name"],
                    "additionalProperties": False,
                },
            }
        }
        req = CompletionRequest(
            messages=[{"role": "user", "content": "hi"}],
            output_config=schema,
        )
        assert req.output_config == schema
        assert req.output_config["format"]["type"] == "json_schema"


from unittest.mock import AsyncMock, MagicMock, patch


class TestAnthropicOutputConfig:
    """Test Anthropic provider injects output_config into API request."""

    @pytest.mark.asyncio
    async def test_output_config_injected_into_api_request(self):
        """output_config should appear in the Anthropic API request dict."""
        from llm_provider.anthropic_provider import AnthropicProvider
        from llm_provider.base import CompletionRequest, ModelTier

        schema = {
            "format": {
                "type": "json_schema",
                "schema": {
                    "type": "object",
                    "properties": {"decision": {"type": "string"}},
                    "required": ["decision"],
                    "additionalProperties": False,
                },
            }
        }

        request = CompletionRequest(
            messages=[
                {"role": "system", "content": "You are a helper."},
                {"role": "user", "content": "Decide something."},
            ],
            model_tier=ModelTier.MEDIUM,
            output_config=schema,
            max_tokens=1024,
        )

        # Capture what gets passed to client.messages.create
        captured_kwargs = {}

        async def mock_create(**kwargs):
            captured_kwargs.update(kwargs)
            mock_resp = MagicMock()
            mock_resp.content = [MagicMock(type="text", text='{"decision": "ok"}')]
            mock_resp.usage = MagicMock(
                input_tokens=10, output_tokens=5,
                cache_read_input_tokens=0, cache_creation_input_tokens=0,
            )
            mock_resp.model = "claude-sonnet-4-6-20250514"
            mock_resp.stop_reason = "end_turn"
            return mock_resp

        provider = AnthropicProvider.__new__(AnthropicProvider)
        provider.config = {}
        provider.models = {
            "claude-sonnet-4-6-20250514": MagicMock(
                model_id="claude-sonnet-4-6-20250514",
                context_window=200000,
                max_tokens=64000,
                supports_functions=True,
                tier=ModelTier.MEDIUM,
            )
        }
        provider.client = MagicMock()
        provider.client.messages.create = mock_create
        provider.estimate_cost = MagicMock(return_value=0.001)

        with patch.object(provider, "select_model_for_tier", return_value=provider.models["claude-sonnet-4-6-20250514"]):
            await provider.complete(request)

        assert "output_config" in captured_kwargs, f"output_config missing from API request. Keys: {list(captured_kwargs.keys())}"
        assert captured_kwargs["output_config"] == schema

    @pytest.mark.asyncio
    async def test_no_output_config_when_not_set(self):
        """output_config should NOT appear in API request when not provided."""
        from llm_provider.anthropic_provider import AnthropicProvider
        from llm_provider.base import CompletionRequest, ModelTier

        request = CompletionRequest(
            messages=[{"role": "user", "content": "Hello"}],
            model_tier=ModelTier.MEDIUM,
            max_tokens=1024,
        )

        captured_kwargs = {}

        async def mock_create(**kwargs):
            captured_kwargs.update(kwargs)
            mock_resp = MagicMock()
            mock_resp.content = [MagicMock(type="text", text="Hello!")]
            mock_resp.usage = MagicMock(
                input_tokens=10, output_tokens=5,
                cache_read_input_tokens=0, cache_creation_input_tokens=0,
            )
            mock_resp.model = "claude-sonnet-4-5-20250929"
            mock_resp.stop_reason = "end_turn"
            return mock_resp

        provider = AnthropicProvider.__new__(AnthropicProvider)
        provider.config = {}
        provider.models = {
            "claude-sonnet-4-5-20250929": MagicMock(
                model_id="claude-sonnet-4-5-20250929",
                context_window=200000,
                max_tokens=64000,
                supports_functions=True,
                tier=ModelTier.MEDIUM,
            )
        }
        provider.client = MagicMock()
        provider.client.messages.create = mock_create
        provider.estimate_cost = MagicMock(return_value=0.001)

        with patch.object(provider, "select_model_for_tier", return_value=provider.models["claude-sonnet-4-5-20250929"]):
            await provider.complete(request)

        assert "output_config" not in captured_kwargs


class TestLeadDecisionSchema:
    """Test the Lead decision JSON schema is valid and matches Pydantic models."""

    def test_schema_importable(self):
        from llm_service.api.lead import LEAD_DECISION_SCHEMA
        assert LEAD_DECISION_SCHEMA is not None
        assert LEAD_DECISION_SCHEMA["format"]["type"] == "json_schema"

    def test_schema_has_required_fields(self):
        from llm_service.api.lead import LEAD_DECISION_SCHEMA
        schema = LEAD_DECISION_SCHEMA["format"]["schema"]
        assert "decision_summary" in schema["properties"]
        assert "actions" in schema["properties"]
        assert set(schema["required"]) == {"decision_summary", "actions"}

    def test_schema_action_type_enum(self):
        from llm_service.api.lead import LEAD_DECISION_SCHEMA
        action_schema = LEAD_DECISION_SCHEMA["format"]["schema"]["properties"]["actions"]["items"]
        action_types = action_schema["properties"]["type"]["enum"]
        expected = [
            "interim_reply", "spawn_agent", "assign_task", "send_message",
            "broadcast", "revise_plan", "file_read", "shutdown_agent",
            "noop", "done", "reply", "synthesize",
        ]
        assert set(action_types) == set(expected)

    def test_schema_all_objects_have_additional_properties_false(self):
        """Anthropic requires additionalProperties: false on all objects."""
        from llm_service.api.lead import LEAD_DECISION_SCHEMA

        def check_object(obj, path="root"):
            if isinstance(obj, dict):
                if obj.get("type") == "object":
                    assert obj.get("additionalProperties") is False, \
                        f"Object at {path} missing additionalProperties: false"
                for k, v in obj.items():
                    check_object(v, f"{path}.{k}")
            elif isinstance(obj, list):
                for i, v in enumerate(obj):
                    check_object(v, f"{path}[{i}]")

        check_object(LEAD_DECISION_SCHEMA)
