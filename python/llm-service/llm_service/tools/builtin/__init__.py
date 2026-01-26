"""
Built-in tools for Shannon platform
"""

from .web_search import WebSearchTool
from .web_fetch import WebFetchTool
from .web_subpage_fetch import WebSubpageFetchTool
from .web_crawl import WebCrawlTool
from .calculator import CalculatorTool
from .file_ops import FileReadTool, FileWriteTool, FileListTool
from .python_wasi_executor import PythonWasiExecutorTool
from .bash_executor import BashExecutorTool


# Private features (enterprise version only) - gracefully degrade if not present
try:
    from .ads_research import AdsSerpExtractTool, AdsTransparencySearchTool, AdsCompetitorDiscoverTool  # noqa: F401
    from .lp_analyze import LPVisualAnalyzeTool  # noqa: F401
    from .ads_creative_analyze import AdsCreativeAnalyzeTool  # noqa: F401
    _HAS_ADS_TOOLS = True
except ImportError:
    _HAS_ADS_TOOLS = False

# Browser automation tools
try:
    from .browser_use import (
        BrowserNavigateTool,
        BrowserClickTool,
        BrowserTypeTool,
        BrowserScreenshotTool,
        BrowserExtractTool,
        BrowserScrollTool,
        BrowserWaitTool,
        BrowserEvaluateTool,
        BrowserCloseTool,
    )
    _HAS_BROWSER_TOOLS = True
except ImportError:
    _HAS_BROWSER_TOOLS = False

__all__ = [
    "WebSearchTool",
    "WebFetchTool",
    "WebSubpageFetchTool",
    "WebCrawlTool",
    "CalculatorTool",
    "FileReadTool",
    "FileWriteTool",
    "FileListTool",
    "BashExecutorTool",
    "PythonWasiExecutorTool",
]


# Add ads tools to exports if available
if _HAS_ADS_TOOLS:
    __all__.extend([
        "AdsSerpExtractTool",
        "AdsTransparencySearchTool",
        "AdsCompetitorDiscoverTool",
        "LPVisualAnalyzeTool",
        "AdsCreativeAnalyzeTool",
    ])

# Add browser tools to exports if available
if _HAS_BROWSER_TOOLS:
    __all__.extend([
        "BrowserNavigateTool",
        "BrowserClickTool",
        "BrowserTypeTool",
        "BrowserScreenshotTool",
        "BrowserExtractTool",
        "BrowserScrollTool",
        "BrowserWaitTool",
        "BrowserEvaluateTool",
        "BrowserCloseTool",
    ])
