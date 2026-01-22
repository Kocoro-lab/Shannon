"""
Tests for web_subpage_fetch keyword-based scoring

Tests the scoring algorithm that selects URLs from Map results
based on target_keywords and high-value path detection.
"""

import pytest
from llm_service.tools.builtin.web_subpage_fetch import WebSubpageFetchTool


class TestKeywordExpansion:
    """Tests for keyword expansion with synonyms."""

    @pytest.fixture
    def tool(self):
        return WebSubpageFetchTool()

    def test_expand_single_keyword(self, tool):
        """Single keyword should expand to include synonyms."""
        expanded = tool._expand_keywords("team")
        assert "team" in expanded
        assert "leadership" in expanded
        assert "management" in expanded
        assert "founders" in expanded

    def test_expand_multiple_keywords(self, tool):
        """Multiple keywords should all expand."""
        expanded = tool._expand_keywords("team funding")
        # team synonyms
        assert "team" in expanded
        assert "leadership" in expanded
        # funding synonyms
        assert "funding" in expanded
        assert "investment" in expanded
        assert "investors" in expanded

    def test_expand_empty_keywords(self, tool):
        """Empty keywords should return empty set."""
        expanded = tool._expand_keywords("")
        assert len(expanded) == 0

        expanded = tool._expand_keywords(None)
        assert len(expanded) == 0

    def test_expand_news_keyword(self, tool):
        """News keyword should include blog, press, etc."""
        expanded = tool._expand_keywords("news")
        assert "news" in expanded
        assert "blog" in expanded
        assert "press" in expanded
        assert "announcements" in expanded


class TestRelevanceScoring:
    """Tests for URL relevance scoring."""

    @pytest.fixture
    def tool(self):
        return WebSubpageFetchTool()

    def test_high_value_path_boost(self, tool):
        """Blog/news/press paths should get high scores."""
        blog_score = tool._calculate_relevance_score(
            "https://example.com/blog/2024-update", "", 50
        )
        generic_score = tool._calculate_relevance_score(
            "https://example.com/random-page", "", 50
        )

        assert blog_score > generic_score
        assert blog_score >= 0.25  # HIGH_VALUE_PATHS boost

    def test_target_keyword_matching(self, tool):
        """URLs matching target keywords should score higher."""
        # With funding keyword
        with_keyword = tool._calculate_relevance_score(
            "https://example.com/investors", "funding", 50
        )
        # Without keyword
        without_keyword = tool._calculate_relevance_score(
            "https://example.com/investors", "", 50
        )

        assert with_keyword > without_keyword

    def test_target_path_matching(self, tool):
        """Target paths should boost relevance scores."""
        with_path = tool._calculate_relevance_score(
            "https://example.com/about/team", "", 50, ["/about"]
        )
        without_path = tool._calculate_relevance_score(
            "https://example.com/about/team", "", 50
        )

        assert with_path > without_path

    def test_depth_scoring(self, tool):
        """Shallow URLs should score higher than deep ones."""
        shallow = tool._calculate_relevance_score(
            "https://example.com/about", "", 50
        )
        deep = tool._calculate_relevance_score(
            "https://example.com/a/b/c/d/e", "", 50
        )

        assert shallow > deep

    def test_base_keyword_scoring(self, tool):
        """URLs with base keywords (about, team, company) should get bonus."""
        about_score = tool._calculate_relevance_score(
            "https://example.com/about", "", 50
        )
        random_score = tool._calculate_relevance_score(
            "https://example.com/xyz", "", 50
        )

        assert about_score > random_score

    def test_combined_scoring(self, tool):
        """Test combined scoring with multiple factors."""
        # Best case: target keyword + high-value path + shallow
        best = tool._calculate_relevance_score(
            "https://example.com/blog/funding-news",
            "funding news",
            50
        )

        # Worst case: no keywords, deep, generic
        worst = tool._calculate_relevance_score(
            "https://example.com/a/b/c/d/e/random-page",
            "funding news",
            50
        )

        assert best > worst
        assert best >= 0.5  # Should be reasonably high


class TestScoringForCompanyResearch:
    """Tests simulating company research scenario."""

    @pytest.fixture
    def tool(self):
        return WebSubpageFetchTool()

    def test_ptmind_like_urls(self, tool):
        """Test scoring for URLs similar to jp.ptmind.com."""
        urls = [
            "https://jp.ptmind.com/",
            "https://jp.ptmind.com/about",
            "https://jp.ptmind.com/company",
            "https://jp.ptmind.com/leadership",
            "https://jp.ptmind.com/blog/2024-funding-announcement",
            "https://jp.ptmind.com/news/product-launch",
            "https://jp.ptmind.com/careers",
            "https://jp.ptmind.com/contact",
            "https://jp.ptmind.com/legal/privacy",
            "https://jp.ptmind.com/legal/terms",
        ]

        keywords = "about team funding news products"

        scores = []
        for url in urls:
            score = tool._calculate_relevance_score(url, keywords, len(urls))
            scores.append((url, score))

        # Sort by score descending
        scores.sort(key=lambda x: x[1], reverse=True)

        # Print for debugging
        print("\nScoring results:")
        for url, score in scores:
            print(f"  {score:.3f} - {url}")

        # High-value content should be in top half
        top_half_urls = [url for url, _ in scores[:len(scores)//2]]

        # blog/news should be boosted
        assert any("blog" in url or "news" in url for url in top_half_urls)

        # about/company/leadership should be high
        assert any("about" in url for url in top_half_urls)

        # legal pages should be low
        bottom_half_urls = [url for url, _ in scores[len(scores)//2:]]
        assert any("legal" in url for url in bottom_half_urls)

    def test_prioritize_funding_content(self, tool):
        """Funding-related content should rank high when keyword provided."""
        urls = [
            "https://example.com/",
            "https://example.com/about",
            "https://example.com/blog/series-b-funding",
            "https://example.com/investors",
            "https://example.com/ir",
            "https://example.com/contact",
        ]

        keywords = "funding investors"

        scores = []
        for url in urls:
            score = tool._calculate_relevance_score(url, keywords, len(urls))
            scores.append((url, score))

        scores.sort(key=lambda x: x[1], reverse=True)

        # funding/investor related should be top 3
        top_3_urls = [url for url, _ in scores[:3]]
        funding_related = sum(1 for url in top_3_urls
                            if any(k in url for k in ["funding", "investor", "ir", "blog"]))

        assert funding_related >= 2, f"Expected 2+ funding-related in top 3, got: {top_3_urls}"


class TestEdgeCases:
    """Edge case tests."""

    @pytest.fixture
    def tool(self):
        return WebSubpageFetchTool()

    def test_empty_path(self, tool):
        """Root URL should still get scored."""
        score = tool._calculate_relevance_score(
            "https://example.com/", "about team", 50
        )
        assert score >= 0.1  # Should have some base score

    def test_very_long_url(self, tool):
        """Very long URLs should not crash."""
        long_url = "https://example.com/" + "a/" * 50 + "page"
        score = tool._calculate_relevance_score(long_url, "about", 50)
        assert 0 <= score <= 1

    def test_special_characters_in_url(self, tool):
        """URLs with special chars should be handled."""
        score = tool._calculate_relevance_score(
            "https://example.com/about-us?ref=home#section",
            "about",
            50
        )
        assert score > 0

    def test_normalize_target_paths(self, tool):
        normalized = tool._normalize_target_paths([
            "/About",
            "https://example.com/team/",
            "careers",
            "/",
        ])
        assert "/about" in normalized
        assert "/team" in normalized
        assert "/careers" in normalized

    def test_case_insensitive_matching(self, tool):
        """Matching should be case insensitive."""
        lower_score = tool._calculate_relevance_score(
            "https://example.com/about", "ABOUT", 50
        )
        upper_score = tool._calculate_relevance_score(
            "https://example.com/ABOUT", "about", 50
        )

        # Both should match
        assert lower_score > 0.1
        assert upper_score > 0.1


class TestHybridSelection:
    """Tests for hybrid URL selection strategy."""

    @pytest.fixture
    def tool(self):
        return WebSubpageFetchTool()

    def test_hybrid_selection_logic(self, tool):
        """Test that hybrid selection prioritizes keyword matches."""
        urls = [
            "https://example.com/",
            "https://example.com/about",        # keyword match
            "https://example.com/team",         # keyword match
            "https://example.com/blog/update",  # high-value path
            "https://example.com/news/press",   # high-value path
            "https://example.com/contact",
            "https://example.com/careers",
            "https://example.com/random-page",
            "https://example.com/another-page",
            "https://example.com/legal/privacy",
        ]

        keywords = "about team"
        expanded = tool._expand_keywords(keywords)

        # Classify URLs as keyword-matched or not
        keyword_matched = []
        others = []

        for u in urls:
            from urllib.parse import urlparse
            path_lower = urlparse(u).path.lower()
            is_match = any(kw in path_lower for kw in expanded)
            score = tool._calculate_relevance_score(u, keywords, len(urls))

            if is_match:
                keyword_matched.append((u, score))
            else:
                others.append((u, score))

        # Verify classification
        keyword_urls = [u for u, _ in keyword_matched]
        assert "https://example.com/about" in keyword_urls
        assert "https://example.com/team" in keyword_urls
        # leadership is a synonym of team
        assert any("leadership" in u or "team" in u for u in keyword_urls)

        print(f"\nKeyword matched ({len(keyword_matched)}): {keyword_urls}")
        print(f"Others ({len(others)}): {[u for u, _ in others]}")

    def test_hybrid_quota_allocation(self, tool):
        """Test 60/40 quota split between keyword and others."""
        limit = 10
        keyword_quota = int(limit * 0.6)  # 6
        other_quota = limit - keyword_quota  # 4

        assert keyword_quota == 6
        assert other_quota == 4

        # With limit=12 (like ptmind case)
        limit = 12
        keyword_quota = int(limit * 0.6)  # 7
        other_quota = limit - keyword_quota  # 5

        assert keyword_quota == 7
        assert other_quota == 5
