"""Tests for _best_match in lead.py (_auto_link_task_ids helper).

Covers:
- Punctuation stripping via regex (dot, comma, etc. no longer block matches)
- 60% threshold rejects short descriptions with only 50% overlap
- Exact substring match still returns immediately
"""

from llm_service.api.lead import _best_match


class TestPunctuationStripping:
    """Verify that punctuation in descriptions does not break word matching."""

    def test_punctuation_does_not_break_matching(self):
        """'pricing.' in task_desc should still match 'pricing' in spawn_desc."""
        available = {"T1": "Analyze competitor pricing. Include all tiers."}
        claimed: set = set()
        # spawn_desc uses the word without trailing period
        spawn_desc = "Analyze competitor pricing and tiers for the report"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "T1", (
            f"Expected T1 but got '{result}'. "
            "Punctuation in task_desc should not block word matching."
        )

    def test_comma_in_description_handled(self):
        """Commas within description words should be stripped correctly."""
        available = {"T2": "Research pricing, features, and availability"}
        claimed: set = set()
        spawn_desc = "Research pricing features and availability in the market"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "T2"

    def test_old_split_would_fail(self):
        """Demonstrate that the old .split() approach would fail on punctuation.

        With .split(), 'pricing.' != 'pricing', so overlaps are undercounted.
        The new regex approach extracts 'pricing' from 'pricing.' correctly.
        """
        import re
        # Old behavior simulation
        task_with_period = "pricing."
        spawn_word = "pricing"
        old_match = task_with_period in spawn_word.split()
        # 'pricing.' is NOT in ['pricing'] — this was the bug
        assert not old_match, "Confirms the old split() approach fails on punctuation"

        # New behavior
        task_words = set(re.findall(r'\w+', task_with_period.lower()))
        spawn_words = set(re.findall(r'\w+', spawn_word.lower()))
        new_match = bool(task_words & spawn_words)
        assert new_match, "New regex approach correctly handles 'pricing.' == 'pricing'"


class TestThresholdTightening:
    """Verify that 60% threshold rejects marginal matches that 50% would accept."""

    def test_short_description_needs_high_overlap(self):
        """1 out of 2 words = 50% overlap → should NOT match at 60% threshold."""
        # task_desc has 2 words: "analyze pricing"
        # spawn_desc shares only 1 word: "analyze" (not "pricing")
        available = {"T1": "analyze pricing"}
        claimed: set = set()
        spawn_desc = "analyze competitors thoroughly"
        result = _best_match(spawn_desc, available, claimed)
        # overlap = {"analyze"} → 1/2 = 50% < 60% threshold → no match
        assert result == "", (
            f"Expected no match (50% < 60% threshold) but got '{result}'"
        )

    def test_60_percent_match_succeeds(self):
        """3 out of 5 words = 60% overlap → should match at 60% threshold."""
        available = {"T1": "analyze competitor pricing strategy thoroughly"}
        claimed: set = set()
        # Shares: analyze, competitor, pricing (3/5 = 60%)
        spawn_desc = "analyze competitor pricing the market broadly"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "T1", (
            f"Expected T1 (60% overlap) but got '{result}'"
        )

    def test_old_50_threshold_would_accept_ambiguous_match(self):
        """Confirm that the old 50% threshold would accept what 60% now rejects."""
        # 1/2 = 50% — old code accepted this, new code rejects it
        task_words = {"analyze", "pricing"}
        spawn_words = {"analyze", "competitors", "thoroughly"}
        overlap = len(spawn_words & task_words)
        score = overlap / len(task_words)
        assert score == 0.5, f"Expected score 0.5, got {score}"
        # Old threshold: 0.5 >= 0.5 → True (false positive)
        assert score >= 0.5, "Old 50% threshold would accept this"
        # New threshold: 0.5 >= 0.6 → False (correctly rejected)
        assert not (score >= 0.6), "New 60% threshold correctly rejects this"


class TestExactSubstringMatch:
    """Verify exact substring matching still returns immediately."""

    def test_exact_substring_still_works(self):
        """When spawn_desc contains the full task_desc as substring, return immediately."""
        available = {
            "T1": "analyze competitor pricing",
            "T2": "write synthesis report",
        }
        claimed: set = set()
        # spawn_desc contains T1's description verbatim
        spawn_desc = "analyze competitor pricing for all major vendors"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "T1", (
            f"Expected T1 via exact substring match, got '{result}'"
        )

    def test_exact_match_skips_claimed(self):
        """A claimed task should not be returned even on exact match."""
        available = {"T1": "analyze competitor pricing"}
        claimed = {"T1"}
        spawn_desc = "analyze competitor pricing"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "", "Claimed task should not be returned"

    def test_exact_match_takes_priority_over_word_overlap(self):
        """Exact substring match should beat higher word-overlap candidates."""
        available = {
            "T1": "research pricing",          # 2/2 = 100% word overlap
            "T2": "analyze competitor pricing strategy",  # exact substring match
        }
        claimed: set = set()
        # spawn_desc contains T2 verbatim as prefix — exact match should win
        spawn_desc = "analyze competitor pricing strategy in detail"
        result = _best_match(spawn_desc, available, claimed)
        assert result == "T2", (
            f"Expected T2 (exact substring) but got '{result}'"
        )

    def test_no_match_returns_empty_string(self):
        """When no task meets the threshold, return empty string."""
        available = {"T1": "completely unrelated task description here"}
        claimed: set = set()
        spawn_desc = "analyze pricing"
        result = _best_match(spawn_desc, available, claimed)
        assert result == ""
