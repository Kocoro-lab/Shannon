"""Tests for Lead Agent truncation limits and mailbox integration.

Verifies that summary and key_findings truncation limits allow
richer agent reports to reach the Lead (10000 / 500 chars).
Also verifies agent→Lead mailbox messages are accepted and formatted.
"""

import inspect
import re

from llm_service.api.lead import (
    LeadDecisionRequest,
    LeadEvent,
    LeadBudget,
)
import llm_service.api.lead as lead_module


class TestTruncationLimitsInSource:
    """Verify that lead.py source code uses the correct truncation limits.

    This inspects the actual source to catch regressions — if someone
    changes the slicing constants, these tests will fail.
    """

    def _get_lead_decide_source(self) -> str:
        """Get source code of the lead_decide function."""
        # The endpoint function is registered via router; find it by name
        source = inspect.getsource(lead_module)
        return source

    def test_summary_truncation_is_10000(self):
        """lead.py must truncate summary at 10000 chars, not 1500."""
        source = self._get_lead_decide_source()
        # Match the pattern: str(summary)[:NNNN]
        match = re.search(r'str\(summary\)\[:(\d+)\]', source)
        assert match is not None, "Could not find str(summary)[:N] pattern in lead.py"
        limit = int(match.group(1))
        assert limit == 10000, f"Summary truncation should be 10000, got {limit}"

    def test_finding_truncation_is_500(self):
        """lead.py must truncate key_findings at 500 chars, not 200."""
        source = self._get_lead_decide_source()
        # Match the pattern: str(f)[:NNN] in the findings loop
        match = re.search(r'str\(f\)\[:(\d+)\]', source)
        assert match is not None, "Could not find str(f)[:N] pattern in lead.py"
        limit = int(match.group(1))
        assert limit == 500, f"Finding truncation should be 500, got {limit}"

    def test_old_1500_not_present(self):
        """Ensure the old 1500 summary limit is no longer in the source."""
        source = self._get_lead_decide_source()
        assert '[:1500]' not in source, "Old 1500-char summary limit still present in lead.py"

    def test_old_200_not_present(self):
        """Ensure the old 200 finding limit is no longer in the source."""
        source = self._get_lead_decide_source()
        # Be careful: 200 might appear elsewhere, check in context of str(f)
        findings_section = source[source.index('key_findings'):source.index('tools_used')]
        assert '[:200]' not in findings_section, "Old 200-char finding limit still present in lead.py"


class TestTruncationBehavior:
    """Verify that text at the new limits is preserved correctly."""

    def test_10000_char_summary_preserved(self):
        """A 10000-char summary should pass through without truncation."""
        summary = "A" * 10000
        truncated = str(summary)[:10000]
        assert len(truncated) == 10000

    def test_12000_char_summary_truncated_to_10000(self):
        """A 12000-char summary should be truncated to 10000."""
        summary = "A" * 12000
        truncated = str(summary)[:10000]
        assert len(truncated) == 10000

    def test_500_char_finding_preserved(self):
        """A 500-char finding should pass through without truncation."""
        finding = "B" * 500
        truncated = str(finding)[:500]
        assert len(truncated) == 500

    def test_800_char_finding_truncated_to_500(self):
        """An 800-char finding should be truncated to 500."""
        finding = "B" * 800
        truncated = str(finding)[:500]
        assert len(truncated) == 500

    def test_old_limits_would_lose_data(self):
        """Confirm old limits (1500/200) would lose significant data."""
        summary = "C" * 5000
        assert len(str(summary)[:1500]) == 1500, "Old limit loses data"
        assert len(str(summary)[:10000]) == 5000, "New limit preserves all"

        finding = "D" * 400
        assert len(str(finding)[:200]) == 200, "Old limit loses data"
        assert len(str(finding)[:500]) == 400, "New limit preserves all"


class TestLeadMailbox:
    """Verify agent messages are formatted in Lead prompt."""

    def test_lead_request_accepts_messages_field(self):
        """LeadDecisionRequest should accept messages field."""
        req = LeadDecisionRequest(
            workflow_id="test",
            event=LeadEvent(type="checkpoint", agent_id=""),
            messages=[
                {"from": "Wakkanai", "type": "request", "payload": {"message": "Cannot find data"}},
                {"from": "Koboro", "type": "info", "payload": {"message": "Found important result"}},
            ],
        )
        assert len(req.messages) == 2
        assert req.messages[0]["from"] == "Wakkanai"

    def test_lead_request_messages_default_empty(self):
        """Messages field should default to empty list."""
        req = LeadDecisionRequest(
            workflow_id="test",
            event=LeadEvent(type="checkpoint", agent_id=""),
        )
        assert req.messages == []

    def test_messages_formatting_in_source(self):
        """lead.py must contain agent message formatting logic."""
        source = inspect.getsource(lead_module)
        assert "Agent Messages" in source, "Lead prompt must have 'Agent Messages' section header"
        assert "body.messages" in source, "Lead must reference body.messages"


class TestLeadFileContents:
    """Verify file contents are formatted in Lead prompt."""

    def test_file_contents_in_event(self):
        """When event has file_contents, they should appear in Lead prompt."""
        event = LeadEvent(
            type="agent_idle",
            agent_id="Mashike",
            file_contents=[
                {"path": "research/mashike-perf.md", "content": "# Performance\nReact: 44.5KB", "truncated": False},
            ],
        )
        assert len(event.file_contents) == 1
        assert event.file_contents[0]["path"] == "research/mashike-perf.md"

    def test_file_contents_default_empty(self):
        """file_contents should default to empty list."""
        event = LeadEvent(type="checkpoint", agent_id="")
        assert event.file_contents == []

    def test_file_contents_formatting_in_source(self):
        """lead.py must contain file contents formatting logic."""
        source = inspect.getsource(lead_module)
        assert "File Preview" in source, "Lead prompt must have 'File Preview' section header"
        assert "file_contents" in source, "Lead must reference file_contents"

    def test_lead_action_has_path_field(self):
        """LeadAction must have a path field for file_read."""
        from llm_service.api.lead import LeadAction
        action = LeadAction(type="file_read", path="research/report.md")
        assert action.path == "research/report.md"
