"""Example of the direct tool API."""

import json
import os

from shannon import ShannonClient


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")
    tool_name = os.getenv("SHANNON_TOOL_NAME")
    raw_arguments = os.getenv("SHANNON_TOOL_ARGUMENTS")

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        tools = client.list_tools()
        print(f"Found {len(tools)} tools")
        for tool in tools[:10]:
            print(f"  - {tool.name}: {tool.description}")

        if not tools:
            return

        target_name = tool_name or next(
            (tool.name for tool in tools if tool.name == "calculator"),
            tools[0].name,
        )
        detail = client.get_tool(target_name)
        print()
        print(f"Tool: {detail.name}")
        print(f"Category: {detail.category}")
        print(f"Description: {detail.description}")

        if raw_arguments:
            arguments = json.loads(raw_arguments)
        elif target_name == "calculator":
            arguments = {"expression": "6 * 7"}
        else:
            print("Set SHANNON_TOOL_ARGUMENTS to execute this tool.")
            return

        result = client.execute_tool(target_name, arguments=arguments)
        print()
        print(f"Success: {result.success}")
        if result.text:
            print(f"Text: {result.text}")
        if result.output is not None:
            print(f"Output: {result.output}")
        if result.error:
            print(f"Error: {result.error}")
    finally:
        client.close()


if __name__ == "__main__":
    main()
