"""
Built-in tools for Shannon platform
"""

from .web_search import WebSearchTool
from .web_fetch import WebFetchTool
from .web_subpage_fetch import WebSubpageFetchTool
from .web_crawl import WebCrawlTool
from .calculator import CalculatorTool
from .file_ops import FileReadTool, FileWriteTool, FileListTool, FileSearchTool, FileEditTool, FileDeleteTool
from .data_tools import DiffFilesTool, JsonQueryTool
from .python_wasi_executor import PythonWasiExecutorTool
from .bash_executor import BashExecutorTool
from .x_search import XSearchTool
from .xquik_search import XquikSearchTool, xquik_search_enabled

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
    "FileDeleteTool",
    "DiffFilesTool",
    "JsonQueryTool",
    "BashExecutorTool",
    "PythonWasiExecutorTool",
    "XSearchTool",
    "XquikSearchTool",
    "xquik_search_enabled",
]

# Add browser tool to exports if available
if _HAS_BROWSER_TOOLS:
    __all__.append("BrowserTool")
