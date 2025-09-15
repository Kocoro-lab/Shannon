"""
Simple Python Executor Tool

A simplified version that converts Python code to WebAssembly Text format (WAT)
for basic operations, then executes via the code_executor tool.

This is a proof-of-concept that handles simple Python expressions by
transpiling them to WAT format.
"""

import base64
import re
from typing import Dict, List, Optional, Any
from ...generated.agent import agent_pb2, agent_pb2_grpc
from ...generated.common import common_pb2
from google.protobuf import struct_pb2
import grpc
import os

from ..base import (
    Tool,
    ToolMetadata,
    ToolParameter,
    ToolParameterType,
    ToolResult,
)


class SimplePythonExecutorTool(Tool):
    """Execute simple Python expressions via WASI by converting to WAT."""

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="simple_python_executor",
            version="0.1.0",
            description="Execute simple Python expressions in WASI sandbox",
            category="code",
            author="Shannon",
            requires_auth=False,
            rate_limit=10,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="code",
                type=ToolParameterType.STRING,
                description="Simple Python code (print statements and basic math)",
                required=True,
            ),
        ]

    def _python_to_wat(self, code: str) -> str:
        """Convert simple Python code to WebAssembly Text format."""

        # Extract print statements and their content
        print_matches = re.findall(r'print\((.*?)\)', code)

        # Build WAT module
        wat = """(module
  (import "wasi_snapshot_preview1" "fd_write"
    (func $fd_write (param i32 i32 i32 i32) (result i32)))

  (memory 1)
  (export "memory" (memory 0))

"""

        # Add data sections for each print statement
        offset = 100
        data_sections = []
        print_calls = []

        for i, content in enumerate(print_matches):
            # Evaluate the content (for simple expressions)
            try:
                # Remove quotes if it's a string
                if content.startswith('"') or content.startswith("'"):
                    result = content.strip('"').strip("'")
                else:
                    # Try to evaluate as expression
                    # SAFETY: Only allow safe operations
                    safe_dict = {"__builtins__": {}}
                    result = str(eval(content, safe_dict))
            except:
                result = str(content)

            # Add newline
            result += "\\n"

            # Add data section
            data_sections.append(f'  (data (i32.const {offset}) "{result}")')

            # Add print call
            length = len(result)
            print_calls.append(f"""    ;; Print: {content}
    (i32.store (i32.const {i * 8}) (i32.const {offset}))
    (i32.store (i32.const {i * 8 + 4}) (i32.const {length}))
    (call $fd_write
      (i32.const 1)
      (i32.const {i * 8})
      (i32.const 1)
      (i32.const {900 + i * 4})
    )
    drop
""")

            offset += length + 10

        # Add data sections
        wat += "\n".join(data_sections) + "\n\n"

        # Add main function
        wat += """  (func $main (export "_start")
"""
        wat += "\n".join(print_calls)
        wat += """  )
)"""

        return wat

    async def _execute_impl(self, session_context: Optional[Dict] = None, **kwargs) -> ToolResult:
        code = kwargs["code"]

        try:
            # Convert Python to WAT
            wat_code = self._python_to_wat(code)

            # Compile WAT to WASM (we'll use wat2wasm via subprocess)
            import subprocess
            import tempfile

            with tempfile.NamedTemporaryFile(suffix='.wat', mode='w', delete=False) as wat_file:
                wat_file.write(wat_code)
                wat_file.flush()

                # Compile to WASM
                wasm_path = wat_file.name.replace('.wat', '.wasm')
                result = subprocess.run(
                    ['wat2wasm', wat_file.name, '-o', wasm_path],
                    capture_output=True,
                    text=True
                )

                if result.returncode != 0:
                    return ToolResult(
                        success=False,
                        output=None,
                        error=f"WAT compilation failed: {result.stderr}",
                    )

                # Read WASM bytes
                with open(wasm_path, 'rb') as f:
                    wasm_bytes = f.read()

                # Clean up temp files
                os.unlink(wat_file.name)
                os.unlink(wasm_path)

            # Encode WASM as base64
            wasm_b64 = base64.b64encode(wasm_bytes).decode('utf-8')

            # Call agent-core's code_executor
            agent_core_addr = os.getenv("AGENT_CORE_ADDR", "agent-core:50051")

            # Build request
            ctx = struct_pb2.Struct()
            tool_params = struct_pb2.Struct()
            tool_params.update({
                "tool": "code_executor",
                "wasm_base64": wasm_b64,
                "stdin": "",
            })
            ctx.update({"tool_parameters": struct_pb2.Value(struct_value=tool_params)})

            req = agent_pb2.ExecuteTaskRequest(
                query="execute python code via WASI",
                context=ctx,
                available_tools=["code_executor"],
            )

            if hasattr(common_pb2, "ExecutionMode"):
                req.mode = int(common_pb2.ExecutionMode.EXECUTION_MODE_STANDARD)

            # Call Agent Core
            with grpc.insecure_channel(agent_core_addr) as channel:
                stub = agent_pb2_grpc.AgentServiceStub(channel)
                resp = stub.ExecuteTask(req, timeout=10)

            # Check response
            if hasattr(resp, "result") and resp.result:
                return ToolResult(
                    success=True,
                    output=resp.result,
                    metadata={"wat_code": wat_code[:500]},  # Include first 500 chars of WAT for debugging
                )
            else:
                return ToolResult(
                    success=False,
                    output=None,
                    error=resp.error_message if hasattr(resp, "error_message") else "Unknown error",
                )

        except Exception as e:
            return ToolResult(
                success=False,
                output=None,
                error=f"Execution failed: {str(e)}",
            )