from llm_service.roles.presets import render_system_prompt


def test_pass_through_when_no_placeholders():
    prompt = "You are a helpful AI assistant."
    out = render_system_prompt(prompt, {"prompt_params": {"target": "./src"}})
    assert out == prompt


def test_missing_variables_render_empty_strings():
    prompt = "Hello ${name}, analyze ${target}"
    out = render_system_prompt(prompt, {"prompt_params": {"target": "./src"}})
    assert out == "Hello , analyze ./src"


def test_precedence_prompt_params_over_tool_parameters():
    prompt = "Analyze ${target} at depth ${depth}"
    ctx = {
        "tool_parameters": {"target": "A", "depth": "shallow"},
        "prompt_params": {"target": "B"},
    }
    out = render_system_prompt(prompt, ctx)
    assert out == "Analyze B at depth shallow"


def test_non_whitelisted_context_not_used():
    prompt = "Role=${role} Target=${target}"
    ctx = {
        "role": "analyst",
        "prompt_params": {"target": "./service"},
    }
    out = render_system_prompt(prompt, ctx)
    # role is not whitelisted; should collapse to empty
    assert out == "Role= Target=./service"

