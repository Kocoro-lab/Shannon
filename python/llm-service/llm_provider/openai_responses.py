"""Utility helpers for working with OpenAI's Responses API."""

from __future__ import annotations

import json
from typing import Any, Dict, Iterable, List, Optional, Union

_ALLOWED_MESSAGE_ROLES = {"user", "assistant", "system", "developer"}


def _ensure_text(value: Any) -> str:
    """Convert arbitrary values into a text string for the Responses API."""
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    try:
        return json.dumps(value)
    except Exception:
        return str(value)


def _convert_content_item(item: Any) -> Any:
    """Normalize chat completion content blocks to Responses input format."""
    if isinstance(item, dict):
        item_type = item.get("type")
        if item_type == "text":
            return {"type": "input_text", "text": _ensure_text(item.get("text", ""))}
        if item_type == "input_text":
            return {"type": "input_text", "text": _ensure_text(item.get("text", ""))}
        if item_type == "image_url":
            image = item.get("image_url") or {}
            converted: Dict[str, Any] = {"type": "input_image"}
            if isinstance(image, dict):
                url = image.get("url")
                if url:
                    converted["image_url"] = url
                detail = image.get("detail")
                if detail:
                    converted["detail"] = detail
            return converted
        if item_type == "input_image":
            # Already in the expected Responses shape
            return item
        # Unknown block types are forwarded as-is to preserve data
        return item
    # Non-dict content becomes simple text
    return {"type": "input_text", "text": _ensure_text(item)}


def prepare_responses_input(messages: Iterable[Dict[str, Any]]) -> List[Dict[str, Any]]:
    """Convert legacy chat-completion style messages into Responses input items."""
    inputs: List[Dict[str, Any]] = []
    fallback_index = 0

    for message in messages or []:
        if not isinstance(message, dict):
            continue

        role = message.get("role")
        content = message.get("content")

        # Tool and function role messages represent tool outputs
        if role in {"tool", "function"}:
            call_id = (
                message.get("tool_call_id")
                or message.get("id")
                or message.get("name")
                or f"tool_call_{fallback_index}"
            )
            fallback_index += 1
            inputs.append(
                {
                    "type": "function_call_output",
                    "call_id": call_id,
                    "output": _ensure_text(content),
                }
            )
            continue

        # Standard conversational messages
        if role in _ALLOWED_MESSAGE_ROLES or role is None:
            if content not in (None, "", []):
                if isinstance(content, list):
                    normalized_content = [_convert_content_item(item) for item in content]
                else:
                    normalized_content = _ensure_text(content)
                inputs.append(
                    {
                        "role": role if role in _ALLOWED_MESSAGE_ROLES else "user",
                        "content": normalized_content,
                    }
                )
        elif isinstance(role, str):
            # Unknown roles are treated as user content to preserve context
            if content not in (None, "", []):
                inputs.append(
                    {
                        "role": "user",
                        "content": _ensure_text(content),
                    }
                )

        # Legacy assistant tool calls may live on the message itself
        function_call = message.get("function_call")
        if function_call:
            call_id = (
                function_call.get("id")
                or function_call.get("call_id")
                or message.get("id")
                or f"call_{fallback_index}"
            )
            fallback_index += 1
            inputs.append(
                {
                    "type": "function_call",
                    "call_id": call_id,
                    "name": function_call.get("name", ""),
                    "arguments": _ensure_text(function_call.get("arguments")),
                }
            )

        # Newer tool-call array format
        tool_calls = message.get("tool_calls") or []
        for tool_call in tool_calls:
            if not isinstance(tool_call, dict):
                continue
            function = tool_call.get("function", {})
            call_id = tool_call.get("id") or function.get("call_id") or f"call_{fallback_index}"
            fallback_index += 1
            inputs.append(
                {
                    "type": "function_call",
                    "call_id": call_id,
                    "name": function.get("name", ""),
                    "arguments": _ensure_text(function.get("arguments")),
                }
            )

    return inputs


def prepare_tools(functions: Optional[Iterable[Dict[str, Any]]]) -> Optional[List[Dict[str, Any]]]:
    """Convert Chat Completions `functions` into Responses `tools`."""
    if not functions:
        return None

    tools: List[Dict[str, Any]] = []
    for fn in functions:
        if isinstance(fn, dict):
            tools.append({"type": "function", "function": fn})
    return tools or None


def prepare_tool_choice(choice: Optional[Union[str, Dict[str, Any]]]) -> Optional[Union[str, Dict[str, str]]]:
    """Normalize Chat Completions `function_call` directives for Responses."""
    if not choice:
        return None

    if isinstance(choice, str):
        return choice

    if isinstance(choice, dict):
        name = choice.get("name")
        if name:
            return {"type": "function", "name": name}

    return None


def build_extra_body(
    *,
    stop: Optional[Union[str, List[str]]] = None,
    frequency_penalty: Optional[float] = None,
    presence_penalty: Optional[float] = None,
    seed: Optional[int] = None,
) -> Optional[Dict[str, Any]]:
    """Bundle parameters that aren't first-class in the Responses SDK."""
    extra: Dict[str, Any] = {}
    if stop:
        extra["stop"] = stop
    if frequency_penalty not in (None, 0):
        extra["frequency_penalty"] = frequency_penalty
    if presence_penalty not in (None, 0):
        extra["presence_penalty"] = presence_penalty
    if seed is not None:
        extra["seed"] = seed
    return extra or None


def extract_output_text(response: Any) -> str:
    """Get text output from a Responses result with graceful fallbacks."""
    text = getattr(response, "output_text", None)
    if text:
        return text

    output = getattr(response, "output", None) or []
    collected: List[str] = []
    for item in output:
        item_type = getattr(item, "type", None) if not isinstance(item, dict) else item.get("type")
        if item_type == "message":
            contents = getattr(item, "content", None)
            if contents is None and isinstance(item, dict):
                contents = item.get("content")
            for block in contents or []:
                block_type = getattr(block, "type", None) if not isinstance(block, dict) else block.get("type")
                if block_type == "output_text":
                    text_value = getattr(block, "text", None) if not isinstance(block, dict) else block.get("text")
                    if text_value:
                        collected.append(text_value)
    return "".join(collected)


def normalize_response_tool_calls(response: Any) -> List[Dict[str, Any]]:
    """Extract tool calls from a Responses result."""
    output = getattr(response, "output", None)
    if output is None and isinstance(response, dict):
        output = response.get("output")

    tool_calls: List[Dict[str, Any]] = []
    for item in output or []:
        source = item
        if isinstance(item, dict):
            item_type = item.get("type")
        else:
            item_type = getattr(item, "type", None)
        if item_type == "function_call":
            if isinstance(source, dict):
                name = source.get("name")
                arguments = source.get("arguments")
                call_id = source.get("call_id") or source.get("id")
            else:
                name = getattr(source, "name", None)
                arguments = getattr(source, "arguments", None)
                call_id = getattr(source, "call_id", None) or getattr(source, "id", None)
            tool_calls.append(
                {
                    "type": "function",
                    "name": name,
                    "arguments": arguments,
                    "id": call_id,
                }
            )
    return tool_calls


def select_primary_function_call(tool_calls: List[Dict[str, Any]]) -> Optional[Dict[str, Any]]:
    """Return the first function call in normalized tool-call lists."""
    if not tool_calls:
        return None
    call = tool_calls[0]
    if not call:
        return None
    name = call.get("name")
    arguments = call.get("arguments")
    if name or arguments:
        return {"name": name, "arguments": arguments}
    return None


def determine_finish_reason(response: Any) -> str:
    """Map Responses status/incomplete details to a legacy finish reason string."""
    status = getattr(response, "status", None)
    if status in (None, "completed"):
        details = getattr(response, "incomplete_details", None)
        reason = getattr(details, "reason", None) if details else None
        return reason or "stop"
    if status == "incomplete":
        details = getattr(response, "incomplete_details", None)
        reason = getattr(details, "reason", None) if details else None
        return reason or "incomplete"
    if status == "failed":
        return "error"
    return str(status)
