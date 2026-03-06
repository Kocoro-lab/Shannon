"""
Built-in tools for Shannon platform
"""

from .web_search import WebSearchTool
from .web_fetch import WebFetchTool
from .web_subpage_fetch import WebSubpageFetchTool
from .web_crawl import WebCrawlTool
from .calculator import CalculatorTool
from .file_ops import FileReadTool, FileWriteTool, FileListTool, FileSearchTool, FileEditTool
from .data_tools import DiffFilesTool, JsonQueryTool
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

# Browser automation tool
try:
    from .browser_use import BrowserTool
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
    "FileSearchTool",
    "FileEditTool",
    "DiffFilesTool",
    "JsonQueryTool",
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

# Add browser tool to exports if available
if _HAS_BROWSER_TOOLS:
    __all__.append("BrowserTool")
