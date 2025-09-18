"""
Tool Registry - Dynamic tool discovery and management
"""

import importlib
import inspect
import os
from pathlib import Path
from typing import Dict, List, Optional, Type
import logging
from .base import Tool, ToolMetadata

logger = logging.getLogger(__name__)


class ToolRegistry:
    """
    Central registry for all available tools.
    Supports dynamic loading and discovery.
    """

    def __init__(self):
        self._tools: Dict[str, Type[Tool]] = {}
        self._instances: Dict[str, Tool] = {}
        self._categories: Dict[str, List[str]] = {}

    def register(self, tool_class: Type[Tool], override: bool = False) -> None:
        """
        Register a tool class

        Args:
            tool_class: The Tool class to register
            override: Whether to override existing tool with same name
        """
        if not issubclass(tool_class, Tool):
            raise TypeError(f"{tool_class} must be a subclass of Tool")

        # Create temporary instance to get metadata
        temp_instance = tool_class()
        metadata = temp_instance.metadata

        if metadata.name in self._tools and not override:
            raise ValueError(f"Tool '{metadata.name}' is already registered")

        self._tools[metadata.name] = tool_class

        # Update category index
        if metadata.category not in self._categories:
            self._categories[metadata.category] = []
        if metadata.name not in self._categories[metadata.category]:
            self._categories[metadata.category].append(metadata.name)

        logger.info(f"Registered tool: {metadata.name} (category: {metadata.category})")

    def unregister(self, tool_name: str) -> None:
        """Unregister a tool"""
        if tool_name in self._tools:
            tool_class = self._tools[tool_name]
            temp_instance = tool_class()
            category = temp_instance.metadata.category

            del self._tools[tool_name]
            if tool_name in self._instances:
                del self._instances[tool_name]

            # Update category index
            if category in self._categories:
                self._categories[category].remove(tool_name)
                if not self._categories[category]:
                    del self._categories[category]

            logger.info(f"Unregistered tool: {tool_name}")

    def get_tool(self, name: str) -> Optional[Tool]:
        """
        Get a tool instance by name.
        Uses singleton pattern - returns same instance for same tool.
        """
        if name not in self._tools:
            return None

        if name not in self._instances:
            self._instances[name] = self._tools[name]()

        return self._instances[name]

    def list_tools(self) -> List[str]:
        """List all registered tool names"""
        return list(self._tools.keys())

    def list_categories(self) -> List[str]:
        """List all tool categories"""
        return list(self._categories.keys())

    def list_tools_by_category(self, category: str) -> List[str]:
        """List tools in a specific category"""
        return self._categories.get(category, [])

    def get_tool_metadata(self, name: str) -> Optional[ToolMetadata]:
        """Get metadata for a tool"""
        tool = self.get_tool(name)
        return tool.metadata if tool else None

    def get_tool_schema(self, name: str) -> Optional[Dict]:
        """Get JSON schema for a tool"""
        tool = self.get_tool(name)
        return tool.get_schema() if tool else None

    def get_all_schemas(self) -> List[Dict]:
        """Get schemas for all registered tools"""
        schemas = []
        for name in self._tools:
            tool = self.get_tool(name)
            if tool:
                schemas.append(tool.get_schema())
        return schemas

    def discover_tools(self, package_path: str) -> int:
        """
        Dynamically discover and register tools from a package.

        Args:
            package_path: Path to package containing tool modules

        Returns:
            Number of tools discovered and registered
        """
        discovered_count = 0

        # Convert to Path object
        path = Path(package_path)

        if not path.exists():
            logger.warning(f"Tool package path does not exist: {package_path}")
            return 0

        # Find all Python files
        for py_file in path.rglob("*.py"):
            if py_file.name.startswith("_"):
                continue

            # Convert file path to module name
            relative_path = py_file.relative_to(path.parent)
            module_name = str(relative_path.with_suffix("")).replace(os.sep, ".")

            try:
                # Import the module
                module = importlib.import_module(module_name)

                # Find all Tool subclasses in the module
                for name, obj in inspect.getmembers(module):
                    if (
                        inspect.isclass(obj)
                        and issubclass(obj, Tool)
                        and obj is not Tool
                    ):
                        try:
                            self.register(obj)
                            discovered_count += 1
                        except ValueError as e:
                            # Tool might already be registered
                            logger.debug(f"Could not register {name}: {e}")
                        except Exception as e:
                            logger.error(f"Error registering {name}: {e}")

            except ImportError as e:
                logger.error(f"Could not import module {module_name}: {e}")
            except Exception as e:
                logger.error(f"Error processing module {module_name}: {e}")

        logger.info(f"Discovered {discovered_count} tools from {package_path}")
        return discovered_count

    def filter_tools_for_agent(
        self,
        categories: Optional[List[str]] = None,
        exclude_dangerous: bool = True,
        max_cost: Optional[float] = None,
    ) -> List[str]:
        """
        Filter tools based on agent requirements.

        Args:
            categories: Only include tools from these categories
            exclude_dangerous: Whether to exclude dangerous tools
            max_cost: Maximum cost per use

        Returns:
            List of tool names that match criteria
        """
        filtered = []

        for name, tool_class in self._tools.items():
            tool = self.get_tool(name)
            if not tool:
                continue

            metadata = tool.metadata

            # Category filter
            if categories and metadata.category not in categories:
                continue

            # Danger filter
            if exclude_dangerous and metadata.dangerous:
                continue

            # Cost filter
            if max_cost is not None and metadata.cost_per_use > max_cost:
                continue

            filtered.append(name)

        return filtered

    def __repr__(self) -> str:
        return f"<ToolRegistry: {len(self._tools)} tools in {len(self._categories)} categories>"


# Global registry singleton
_global_registry = None


def get_registry() -> ToolRegistry:
    """Get the global tool registry singleton"""
    global _global_registry
    if _global_registry is None:
        _global_registry = ToolRegistry()
    return _global_registry
