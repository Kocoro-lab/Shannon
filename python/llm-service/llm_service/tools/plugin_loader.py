"""
Dynamic Plugin Loader for Shannon Tool System
Supports hot reloading and external tool plugins
"""

import os
import sys
import importlib
import importlib.util
from pathlib import Path
from typing import Dict, List, Optional, Type
import logging
import hashlib
from watchdog.observers import Observer
from watchdog.events import FileSystemEventHandler
import asyncio

from .base import Tool
from .registry import get_registry

logger = logging.getLogger(__name__)


class ToolPluginLoader:
    """
    Dynamic tool plugin loader with hot reloading support
    """
    
    def __init__(self, plugin_dirs: Optional[List[str]] = None):
        """
        Initialize plugin loader
        
        Args:
            plugin_dirs: List of directories to search for plugins
        """
        self.plugin_dirs = plugin_dirs or []
        self.loaded_plugins: Dict[str, LoadedPlugin] = {}
        self.observer = None
        self.registry = get_registry()
        
        # Add default plugin directories
        default_dirs = [
            os.path.join(os.path.dirname(__file__), "builtin"),
            os.path.join(os.path.dirname(__file__), "community"),
            os.path.expanduser("~/.shannon/tools"),
            "/opt/shannon/tools",
        ]
        
        for dir_path in default_dirs:
            if os.path.exists(dir_path) and dir_path not in self.plugin_dirs:
                self.plugin_dirs.append(dir_path)
    
    def discover_and_load_all(self) -> int:
        """
        Discover and load all tools from configured directories
        
        Returns:
            Number of tools loaded
        """
        total_loaded = 0
        
        for plugin_dir in self.plugin_dirs:
            if not os.path.exists(plugin_dir):
                logger.debug(f"Plugin directory does not exist: {plugin_dir}")
                continue
            
            logger.info(f"Scanning plugin directory: {plugin_dir}")
            loaded = self._load_plugins_from_directory(plugin_dir)
            total_loaded += loaded
        
        logger.info(f"Total tools loaded: {total_loaded}")
        return total_loaded
    
    def _load_plugins_from_directory(self, directory: str) -> int:
        """
        Load all plugin files from a directory
        
        Args:
            directory: Directory to scan for plugins
            
        Returns:
            Number of plugins loaded
        """
        loaded_count = 0
        path = Path(directory)
        
        # Find all Python files
        for py_file in path.rglob("*.py"):
            if py_file.name.startswith("_") or py_file.name == "setup.py":
                continue
            
            try:
                if self._load_plugin_file(str(py_file)):
                    loaded_count += 1
            except Exception as e:
                logger.error(f"Failed to load plugin {py_file}: {e}")
        
        # Find all .shannon-tool files (JSON tool definitions)
        for tool_file in path.rglob("*.shannon-tool"):
            try:
                if self._load_json_tool(str(tool_file)):
                    loaded_count += 1
            except Exception as e:
                logger.error(f"Failed to load tool definition {tool_file}: {e}")
        
        return loaded_count
    
    def _load_plugin_file(self, file_path: str) -> bool:
        """
        Load a single plugin file
        
        Args:
            file_path: Path to the plugin file
            
        Returns:
            True if successfully loaded
        """
        # Check if already loaded and unchanged
        file_hash = self._get_file_hash(file_path)
        
        if file_path in self.loaded_plugins:
            plugin = self.loaded_plugins[file_path]
            if plugin.file_hash == file_hash:
                logger.debug(f"Plugin {file_path} unchanged, skipping")
                return False
            else:
                # File changed, reload it
                logger.info(f"Reloading changed plugin: {file_path}")
                self._unload_plugin(file_path)
        
        # Load the module
        try:
            spec = importlib.util.spec_from_file_location(
                f"shannon_tool_plugin_{Path(file_path).stem}",
                file_path
            )
            
            if spec and spec.loader:
                module = importlib.util.module_from_spec(spec)
                sys.modules[spec.name] = module
                spec.loader.exec_module(module)
                
                # Find all Tool subclasses
                tools_found = []
                for name in dir(module):
                    obj = getattr(module, name)
                    if (isinstance(obj, type) and 
                        issubclass(obj, Tool) and 
                        obj is not Tool):
                        
                        # Register the tool
                        try:
                            self.registry.register(obj, override=True)
                            tools_found.append(obj)
                            logger.info(f"Loaded tool: {obj.__name__} from {file_path}")
                        except Exception as e:
                            logger.error(f"Failed to register tool {obj.__name__}: {e}")
                
                if tools_found:
                    self.loaded_plugins[file_path] = LoadedPlugin(
                        file_path=file_path,
                        file_hash=file_hash,
                        module=module,
                        tools=tools_found,
                    )
                    return True
                
        except Exception as e:
            logger.error(f"Failed to load module {file_path}: {e}")
        
        return False
    
    def _load_json_tool(self, file_path: str) -> bool:
        """
        Load a tool defined in JSON format
        
        Args:
            file_path: Path to the JSON tool definition
            
        Returns:
            True if successfully loaded
        """
        import json
        
        try:
            with open(file_path, 'r') as f:
                tool_def = json.load(f)
            
            # Create a dynamic Tool class from JSON definition
            tool_class = self._create_tool_from_json(tool_def)
            
            if tool_class:
                self.registry.register(tool_class, override=True)
                logger.info(f"Loaded JSON tool: {tool_def.get('name')} from {file_path}")
                return True
                
        except Exception as e:
            logger.error(f"Failed to load JSON tool {file_path}: {e}")
        
        return False
    
    def _create_tool_from_json(self, tool_def: dict) -> Optional[Type[Tool]]:
        """
        Create a Tool class from JSON definition
        
        Args:
            tool_def: Tool definition dictionary
            
        Returns:
            Dynamically created Tool class or None
        """
        from .base import ToolMetadata, ToolParameter, ToolParameterType, ToolResult
        
        # Extract tool information
        name = tool_def.get("name", "unknown")
        
        # Create the tool class dynamically
        class JSONDefinedTool(Tool):
            def _get_metadata(self) -> ToolMetadata:
                return ToolMetadata(
                    name=tool_def.get("name"),
                    version=tool_def.get("version", "1.0.0"),
                    description=tool_def.get("description", ""),
                    category=tool_def.get("category", "general"),
                    author=tool_def.get("author", "Community"),
                    requires_auth=tool_def.get("requires_auth", False),
                    rate_limit=tool_def.get("rate_limit"),
                    timeout_seconds=tool_def.get("timeout_seconds", 30),
                    memory_limit_mb=tool_def.get("memory_limit_mb", 256),
                    sandboxed=tool_def.get("sandboxed", True),
                    dangerous=tool_def.get("dangerous", False),
                    cost_per_use=tool_def.get("cost_per_use", 0.0),
                )
            
            def _get_parameters(self) -> List[ToolParameter]:
                params = []
                for param_def in tool_def.get("parameters", []):
                    params.append(ToolParameter(
                        name=param_def["name"],
                        type=ToolParameterType[param_def["type"].upper()],
                        description=param_def.get("description", ""),
                        required=param_def.get("required", True),
                        default=param_def.get("default"),
                        enum=param_def.get("enum"),
                        min_value=param_def.get("min_value"),
                        max_value=param_def.get("max_value"),
                        pattern=param_def.get("pattern"),
                    ))
                return params
            
            async def _execute_impl(self, **kwargs) -> ToolResult:
                # For JSON-defined tools, we need an execution strategy
                # This could be a script, API call, or command
                execution = tool_def.get("execution", {})
                exec_type = execution.get("type", "script")
                
                if exec_type == "script":
                    # Execute a script
                    script_path = execution.get("script")
                    if script_path:
                        import subprocess
                        import json
                        
                        try:
                            result = subprocess.run(
                                [script_path],
                                input=json.dumps(kwargs),
                                capture_output=True,
                                text=True,
                                timeout=self.metadata.timeout_seconds,
                            )
                            
                            if result.returncode == 0:
                                output = json.loads(result.stdout) if result.stdout else None
                                return ToolResult(success=True, output=output)
                            else:
                                return ToolResult(success=False, output=None, error=result.stderr)
                                
                        except Exception as e:
                            return ToolResult(success=False, output=None, error=str(e))
                
                elif exec_type == "api":
                    # Make an API call
                    import aiohttp
                    
                    url = execution.get("url")
                    method = execution.get("method", "POST")
                    
                    if url:
                        try:
                            async with aiohttp.ClientSession() as session:
                                async with session.request(method, url, json=kwargs) as response:
                                    if response.status == 200:
                                        output = await response.json()
                                        return ToolResult(success=True, output=output)
                                    else:
                                        error = await response.text()
                                        return ToolResult(success=False, output=None, error=error)
                        except Exception as e:
                            return ToolResult(success=False, output=None, error=str(e))
                
                # Default: not implemented
                return ToolResult(
                    success=False,
                    output=None,
                    error="Execution strategy not implemented"
                )
        
        # Set the class name
        JSONDefinedTool.__name__ = f"{name.replace('-', '_').title()}Tool"
        
        return JSONDefinedTool
    
    def _unload_plugin(self, file_path: str):
        """
        Unload a plugin and its tools
        
        Args:
            file_path: Path to the plugin file
        """
        if file_path in self.loaded_plugins:
            plugin = self.loaded_plugins[file_path]
            
            # Unregister all tools from this plugin
            for tool_class in plugin.tools:
                try:
                    tool_instance = tool_class()
                    self.registry.unregister(tool_instance.metadata.name)
                    logger.info(f"Unregistered tool: {tool_instance.metadata.name}")
                except Exception as e:
                    logger.error(f"Failed to unregister tool: {e}")
            
            # Remove from loaded plugins
            del self.loaded_plugins[file_path]
            
            # Remove module from sys.modules
            if plugin.module and hasattr(plugin.module, "__name__"):
                module_name = plugin.module.__name__
                if module_name in sys.modules:
                    del sys.modules[module_name]
    
    def _get_file_hash(self, file_path: str) -> str:
        """
        Get hash of file contents for change detection
        
        Args:
            file_path: Path to file
            
        Returns:
            SHA256 hash of file contents
        """
        hasher = hashlib.sha256()
        try:
            with open(file_path, 'rb') as f:
                hasher.update(f.read())
            return hasher.hexdigest()
        except Exception:
            return ""
    
    def enable_hot_reload(self):
        """
        Enable hot reloading of plugins when files change
        """
        if self.observer:
            return  # Already enabled
        
        self.observer = Observer()
        handler = PluginFileHandler(self)
        
        for plugin_dir in self.plugin_dirs:
            if os.path.exists(plugin_dir):
                self.observer.schedule(handler, plugin_dir, recursive=True)
                logger.info(f"Watching for changes in: {plugin_dir}")
        
        self.observer.start()
        logger.info("Hot reload enabled for tool plugins")
    
    def disable_hot_reload(self):
        """
        Disable hot reloading
        """
        if self.observer:
            self.observer.stop()
            self.observer.join()
            self.observer = None
            logger.info("Hot reload disabled")
    
    def add_plugin_directory(self, directory: str):
        """
        Add a new plugin directory and load its tools
        
        Args:
            directory: Directory path to add
        """
        if directory not in self.plugin_dirs:
            self.plugin_dirs.append(directory)
            
            # Load plugins from new directory
            loaded = self._load_plugins_from_directory(directory)
            logger.info(f"Added plugin directory {directory}, loaded {loaded} tools")
            
            # Add to file watcher if hot reload is enabled
            if self.observer and self.observer.is_alive():
                handler = PluginFileHandler(self)
                self.observer.schedule(handler, directory, recursive=True)


class LoadedPlugin:
    """Information about a loaded plugin"""
    
    def __init__(self, file_path: str, file_hash: str, module, tools: List[Type[Tool]]):
        self.file_path = file_path
        self.file_hash = file_hash
        self.module = module
        self.tools = tools


class PluginFileHandler(FileSystemEventHandler):
    """File system event handler for hot reloading"""
    
    def __init__(self, loader: ToolPluginLoader):
        self.loader = loader
    
    def on_modified(self, event):
        if event.is_directory:
            return
        
        if event.src_path.endswith('.py') or event.src_path.endswith('.shannon-tool'):
            logger.info(f"Detected change in: {event.src_path}")
            
            # Reload the plugin asynchronously
            asyncio.create_task(self._reload_plugin(event.src_path))
    
    async def _reload_plugin(self, file_path: str):
        """Reload a plugin file"""
        await asyncio.sleep(0.5)  # Small delay to ensure file write is complete
        
        if file_path.endswith('.py'):
            self.loader._load_plugin_file(file_path)
        elif file_path.endswith('.shannon-tool'):
            self.loader._load_json_tool(file_path)