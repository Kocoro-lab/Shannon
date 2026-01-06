import pytest

from llm_service.api.agent import _extract_urls_from_search_output


def test_extract_urls_from_search_output_dedupes_and_filters():
    output = {
        "results": [
            {"url": "https://example.com"},
            {"url": "https://example.com"},
            {"link": "https://another.com/page"},
            {"url": None},
        ]
    }
    urls = _extract_urls_from_search_output(output)
    assert urls == ["https://example.com", "https://another.com/page"]


def test_extract_urls_from_search_output_handles_empty():
    assert _extract_urls_from_search_output({}) == []
    assert _extract_urls_from_search_output(None) == []
