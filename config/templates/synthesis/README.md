# Synthesis Templates

This directory contains Go `text/template` files for synthesis prompt generation.

## Template Selection

Templates are selected based on context and workflow type:

| Condition | Template |
|-----------|----------|
| `context["synthesis_template"]` specified | Named template |
| `workflow_type == "research"` | `research_comprehensive.tmpl` |
| `force_research == true` | `research_comprehensive.tmpl` |
| `synthesis_style == "comprehensive"` | `research_comprehensive.tmpl` |
| `research_areas` present (non-empty) | `research_comprehensive.tmpl` |
| `synthesis_style == "concise"` | `research_concise.tmpl` |
| Default | `normal_default.tmpl` |

**Note**: Role-based agents (e.g., `deep_research_agent`) influence template selection indirectly via context flags. ResearchWorkflow sets `synthesis_style = "comprehensive"` and populates `research_areas`, which triggers the research template.

## Override Precedence

```
context["synthesis_template_override"]  (verbatim text)
    ↓
context["synthesis_template"]  (named template file)
    ↓
Auto-selected based on workflow signals
    ↓
Hardcoded fallback (backward compatibility)
```

**Warning**: `synthesis_template_override` is a power tool that bypasses `_base.tmpl` entirely. When using it, the caller is responsible for including citation instructions (`[n]` format) if citations are needed. The underlying citation extraction and SSE payload logic still works, but the LLM won't be reminded of the format unless you include it in your override text.

## Available Templates

### `_base.tmpl`
Protected system contract. Defines citation format and core requirements that ALL templates must include. **Do not modify** - this is Tier 4's protected layer.

### `research_comprehensive.tmpl`
Full deep research synthesis with:
- Strict section structure (Executive Summary, Detailed Findings, Limitations)
- Per-area subsection requirements
- Citation integration rules
- Quantitative synthesis guidelines

### `research_concise.tmpl`
Shorter research synthesis with:
- Same section structure but lighter word requirements
- Less strict per-area coverage
- Suitable for quick research tasks

### `normal_default.tmpl`
Simple task synthesis:
- Minimal structure
- Concise, direct answers
- Optional citations if available

## Template Variables

All templates receive these variables:

| Variable | Type | Description |
|----------|------|-------------|
| `.Query` | string | Original user query |
| `.QueryLanguage` | string | Detected query language |
| `.ResearchAreas` | []string | Research areas to cover |
| `.AvailableCitations` | string | Formatted citation list |
| `.CitationCount` | int | Number of available citations |
| `.MinCitations` | int | Minimum citations target |
| `.LanguageInstruction` | string | Language matching instruction |
| `.AgentResults` | string | Reserved for future use (currently empty) |
| `.TargetWords` | int | Target word count |
| `.IsResearch` | bool | Whether this is a research workflow |
| `.SynthesisStyle` | string | Style hint (comprehensive, concise, or empty) |

**Note on agent results**: Templates define *instructions and structure* for the LLM. The system appends formatted agent outputs separately after the rendered template. This separation keeps templates focused on behavioral requirements rather than data formatting.

## Custom Templates

To create a custom template:

1. Create a new `.tmpl` file in this directory
2. Include `{{- template "system_contract" . -}}` at the start
3. Include `{{ template "citation_list" . }}` where citations should appear
4. Use context to select: `context["synthesis_template"] = "my_custom"`

## Protected Contract

The `_base.tmpl` defines the citation contract that MUST remain stable:
- Inline citations use `[n]` format
- Citation numbers match the `available_citations` array indices
- No `## Sources` section (system appends automatically)

This ensures SSE payload consistency for the frontend.
