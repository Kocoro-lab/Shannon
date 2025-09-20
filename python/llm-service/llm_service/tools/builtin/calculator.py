"""
Calculator Tool - Safe mathematical expression evaluation
"""

import ast
import operator
import math
from typing import List, Any

from typing import Dict, Optional

from ..base import Tool, ToolMetadata, ToolParameter, ToolParameterType, ToolResult
from ...config import Settings


# Safe operators for expression evaluation
SAFE_OPERATORS = {
    ast.Add: operator.add,
    ast.Sub: operator.sub,
    ast.Mult: operator.mul,
    ast.Div: operator.truediv,
    ast.Pow: operator.pow,
    ast.Mod: operator.mod,
    ast.FloorDiv: operator.floordiv,
    ast.USub: operator.neg,
    ast.UAdd: operator.pos,
}

# Safe math functions
SAFE_FUNCTIONS = {
    "abs": abs,
    "round": round,
    "min": min,
    "max": max,
    "sum": sum,
    "len": len,
    "sqrt": math.sqrt,
    "sin": math.sin,
    "cos": math.cos,
    "tan": math.tan,
    "log": math.log,
    "log10": math.log10,
    "exp": math.exp,
    "pow": pow,
    "pi": math.pi,
    "e": math.e,
    "floor": math.floor,
    "ceil": math.ceil,
}


class CalculatorTool(Tool):
    """
    Safe calculator that can evaluate mathematical expressions.
    Uses AST parsing to ensure safety - no arbitrary code execution.
    """

    def __init__(self):
        self.settings = Settings()
        super().__init__()

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="calculator",
            version="1.0.0",
            description="Evaluate mathematical expressions safely",
            category="calculation",
            author="Shannon",
            requires_auth=False,
            rate_limit=self.settings.calculator_rate_limit,  # Configurable via CALCULATOR_RATE_LIMIT env var
            timeout_seconds=5,
            memory_limit_mb=64,
            sandboxed=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="expression",
                type=ToolParameterType.STRING,
                description="Mathematical expression to evaluate (e.g., '2 + 2', 'sqrt(16)', 'sin(pi/2)')",
                required=True,
            ),
            ToolParameter(
                name="precision",
                type=ToolParameterType.INTEGER,
                description="Number of decimal places for the result",
                required=False,
                default=6,
                min_value=0,
                max_value=15,
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """
        Safely evaluate mathematical expression
        """
        expression = kwargs["expression"]
        precision = kwargs.get("precision", 6)

        try:
            # Parse and evaluate the expression safely
            result = self._safe_eval(expression)

            # Format the result based on precision
            if isinstance(result, float):
                if precision == 0:
                    result = int(round(result))
                else:
                    result = round(result, precision)

            return ToolResult(
                success=True,
                output=result,
                metadata={
                    "expression": expression,
                    "result_type": type(result).__name__,
                    "precision": precision,
                },
            )

        except ZeroDivisionError:
            return ToolResult(success=False, output=None, error="Division by zero")
        except ValueError as e:
            return ToolResult(
                success=False, output=None, error=f"Math domain error: {str(e)}"
            )
        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Invalid expression: {str(e)}"
            )

    def _safe_eval(self, expression: str) -> Any:
        """
        Safely evaluate a mathematical expression using AST parsing.
        Only allows mathematical operations, no arbitrary code execution.
        """
        # Parse the expression into an AST
        try:
            node = ast.parse(expression, mode="eval")
        except SyntaxError as e:
            raise ValueError(f"Invalid expression syntax: {e}")

        # Evaluate the AST safely
        return self._eval_node(node.body)

    def _eval_node(self, node: ast.AST) -> Any:
        """
        Recursively evaluate an AST node.
        Only allows safe operations.
        """
        if isinstance(node, ast.Constant):  # Python 3.8+
            return node.value
        elif isinstance(node, ast.Num):  # For older Python versions
            return node.n
        elif isinstance(node, ast.Name):
            # Allow only safe constants and functions
            if node.id in SAFE_FUNCTIONS:
                value = SAFE_FUNCTIONS[node.id]
                # If it's a constant (like pi or e), return the value directly
                if not callable(value):
                    return value
                # If it's a function, return it for later calling
                return value
            else:
                raise ValueError(f"Unknown variable or function: {node.id}")
        elif isinstance(node, ast.BinOp):
            # Binary operations like +, -, *, /
            if type(node.op) not in SAFE_OPERATORS:
                raise ValueError(f"Unsafe operator: {type(node.op).__name__}")
            left = self._eval_node(node.left)
            right = self._eval_node(node.right)
            return SAFE_OPERATORS[type(node.op)](left, right)
        elif isinstance(node, ast.UnaryOp):
            # Unary operations like -, +
            if type(node.op) not in SAFE_OPERATORS:
                raise ValueError(f"Unsafe unary operator: {type(node.op).__name__}")
            operand = self._eval_node(node.operand)
            return SAFE_OPERATORS[type(node.op)](operand)
        elif isinstance(node, ast.Call):
            # Function calls
            if isinstance(node.func, ast.Name):
                func_name = node.func.id
                if func_name not in SAFE_FUNCTIONS:
                    raise ValueError(f"Unsafe function: {func_name}")
                func = SAFE_FUNCTIONS[func_name]
                args = [self._eval_node(arg) for arg in node.args]
                return func(*args)
            else:
                raise ValueError("Complex function calls not allowed")
        elif isinstance(node, ast.List):
            # Lists for functions like sum(), min(), max()
            return [self._eval_node(elem) for elem in node.elts]
        elif isinstance(node, ast.Tuple):
            # Tuples
            return tuple(self._eval_node(elem) for elem in node.elts)
        else:
            raise ValueError(f"Unsafe node type: {type(node).__name__}")


class StatisticalCalculatorTool(Tool):
    """
    Advanced statistical calculations
    """

    def _get_metadata(self) -> ToolMetadata:
        return ToolMetadata(
            name="statistics",
            version="1.0.0",
            description="Perform statistical calculations on datasets",
            category="calculation",
            author="Shannon",
            requires_auth=False,
            rate_limit=100,
            timeout_seconds=10,
            memory_limit_mb=256,
            sandboxed=True,
            dangerous=False,
            cost_per_use=0.0,
        )

    def _get_parameters(self) -> List[ToolParameter]:
        return [
            ToolParameter(
                name="data",
                type=ToolParameterType.ARRAY,
                description="Array of numerical values",
                required=True,
            ),
            ToolParameter(
                name="operation",
                type=ToolParameterType.STRING,
                description="Statistical operation to perform",
                required=True,
                enum=[
                    "mean",
                    "median",
                    "mode",
                    "std",
                    "variance",
                    "min",
                    "max",
                    "sum",
                    "count",
                ],
            ),
        ]

    async def _execute_impl(
        self, session_context: Optional[Dict] = None, **kwargs
    ) -> ToolResult:
        """
        Perform statistical calculation
        """
        import statistics

        data = kwargs["data"]
        operation = kwargs["operation"]

        if not data:
            return ToolResult(success=False, output=None, error="Empty dataset")

        try:
            # Ensure all values are numeric
            numeric_data = [float(x) for x in data]

            result = None
            if operation == "mean":
                result = statistics.mean(numeric_data)
            elif operation == "median":
                result = statistics.median(numeric_data)
            elif operation == "mode":
                try:
                    result = statistics.mode(numeric_data)
                except statistics.StatisticsError:
                    result = "No unique mode"
            elif operation == "std":
                if len(numeric_data) > 1:
                    result = statistics.stdev(numeric_data)
                else:
                    result = 0.0
            elif operation == "variance":
                if len(numeric_data) > 1:
                    result = statistics.variance(numeric_data)
                else:
                    result = 0.0
            elif operation == "min":
                result = min(numeric_data)
            elif operation == "max":
                result = max(numeric_data)
            elif operation == "sum":
                result = sum(numeric_data)
            elif operation == "count":
                result = len(numeric_data)

            return ToolResult(
                success=True,
                output=result,
                metadata={
                    "operation": operation,
                    "data_points": len(numeric_data),
                    "data_range": [min(numeric_data), max(numeric_data)]
                    if numeric_data
                    else [None, None],
                },
            )

        except ValueError as e:
            return ToolResult(
                success=False, output=None, error=f"Invalid data: {str(e)}"
            )
        except Exception as e:
            return ToolResult(
                success=False, output=None, error=f"Calculation error: {str(e)}"
            )
