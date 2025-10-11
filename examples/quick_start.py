#!/usr/bin/env python3
"""
Shannon Quick Start Example

This example demonstrates the basic usage of Shannon for common tasks.

Requirements:
    pip install -e clients/python

Usage:
    python examples/quick_start.py
"""

import asyncio
import sys
from pathlib import Path

# Add parent directory to path
sys.path.insert(0, str(Path(__file__).parent.parent / "clients" / "python" / "src"))

from shannon.client import AsyncShannonClient


async def example_1_simple_task():
    """Example 1: Submit a simple task"""
    print("\n" + "=" * 60)
    print("Example 1: Simple Task Submission")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"  # Or use bearer_token for auth
    )
    
    try:
        # Submit a simple task
        task = await client.submit_task(
            query="Calculate the factorial of 10",
            user_id="example-user"
        )
        
        print(f"✅ Task submitted: {task.task_id}")
        print(f"   Workflow ID: {task.workflow_id}")
        
        # Wait for completion (with timeout)
        result = await task.get_result(timeout=30.0)
        
        print(f"\n📊 Task Result:")
        print(f"   Status: {result.status}")
        print(f"   Output: {result.output[:200]}...")  # First 200 chars
        
        if result.cost:
            print(f"   Cost: ${result.cost:.4f}")
            print(f"   Tokens: {result.tokens_used}")
        
        return result
        
    except Exception as e:
        print(f"❌ Error: {e}")
        return None
    finally:
        await client.close()


async def example_2_streaming():
    """Example 2: Stream task progress in real-time"""
    print("\n" + "=" * 60)
    print("Example 2: Streaming Task Progress")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"
    )
    
    try:
        # Submit task
        task = await client.submit_task(
            query="Write a short story about AI agents",
            user_id="example-user"
        )
        
        print(f"✅ Task submitted: {task.task_id}")
        print("\n📡 Streaming events:\n")
        
        # Stream events
        async for event in task.stream_events():
            event_type = event.get("type", "unknown")
            
            if event_type == "task_started":
                print(f"🚀 Task started")
            
            elif event_type == "agent_thinking":
                agent = event.get("agent_id", "unknown")
                print(f"💭 Agent {agent}: {event.get('thought', '')[:50]}...")
            
            elif event_type == "tool_called":
                tool = event.get("tool_name", "unknown")
                print(f"🔧 Tool called: {tool}")
            
            elif event_type == "progress":
                progress = event.get("progress", 0)
                print(f"⏳ Progress: {progress}%")
            
            elif event_type == "task_completed":
                print(f"✅ Task completed!")
                output = event.get("output", "")
                print(f"\n📄 Output:\n{output[:300]}...")
                break
            
            elif event_type == "error":
                print(f"❌ Error: {event.get('error')}")
                break
        
    except Exception as e:
        print(f"❌ Error: {e}")
    finally:
        await client.close()


async def example_3_with_session():
    """Example 3: Multi-turn conversation with session memory"""
    print("\n" + "=" * 60)
    print("Example 3: Conversational Agent with Memory")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"
    )
    
    try:
        session_id = "example-session-001"
        
        # First turn
        print("\n🧑 Turn 1: Tell me about Shannon")
        task1 = await client.submit_task(
            query="Tell me about Shannon framework in one sentence",
            session_id=session_id,
            user_id="example-user"
        )
        result1 = await task1.get_result(timeout=30.0)
        print(f"🤖 Shannon: {result1.output}")
        
        # Second turn - uses session memory
        print("\n🧑 Turn 2: What did I just ask about?")
        task2 = await client.submit_task(
            query="What did I just ask you about?",
            session_id=session_id,
            user_id="example-user"
        )
        result2 = await task2.get_result(timeout=30.0)
        print(f"🤖 Shannon: {result2.output}")
        
        # Third turn
        print("\n🧑 Turn 3: What are its main features?")
        task3 = await client.submit_task(
            query="What are its main features?",
            session_id=session_id,
            user_id="example-user"
        )
        result3 = await task3.get_result(timeout=30.0)
        print(f"🤖 Shannon: {result3.output}")
        
    except Exception as e:
        print(f"❌ Error: {e}")
    finally:
        await client.close()


async def example_4_template_workflow():
    """Example 4: Execute a predefined template workflow"""
    print("\n" + "=" * 60)
    print("Example 4: Template-Based Workflow")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"
    )
    
    try:
        # Execute a template (zero-token execution)
        task = await client.submit_task(
            query="Analyze recent AI developments",
            template_name="simple_analysis",
            template_version="1.0.0",
            disable_ai=False,  # Set to True for pure template execution
            user_id="example-user"
        )
        
        print(f"✅ Template task submitted: {task.task_id}")
        print("   Using template: simple_analysis v1.0.0")
        
        # Stream progress
        async for event in task.stream_events():
            if event.get("type") == "node_started":
                node = event.get("node_id", "unknown")
                print(f"📦 Node started: {node}")
            
            elif event.get("type") == "node_completed":
                node = event.get("node_id", "unknown")
                print(f("✅ Node completed: {node}")
            
            elif event.get("type") == "task_completed":
                print(f"\n✅ Workflow completed!")
                break
        
        result = await task.get_result(timeout=60.0)
        print(f"\n📊 Result: {result.output[:200]}...")
        
    except Exception as e:
        print(f"❌ Error: {e}")
    finally:
        await client.close()


async def example_5_with_tools():
    """Example 5: Task with specific tools"""
    print("\n" + "=" * 60)
    print("Example 5: Task with Specific Tools")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"
    )
    
    try:
        # Task that requires web search
        task = await client.submit_task(
            query="What are the latest AI news headlines today?",
            context={
                "allowed_tools": ["web_search"],
                "cognitive_strategy": "react"
            },
            user_id="example-user"
        )
        
        print(f"✅ Task submitted with web_search tool")
        
        # Stream to see tool usage
        tool_calls = []
        async for event in task.stream_events():
            if event.get("type") == "tool_called":
                tool = event.get("tool_name")
                args = event.get("arguments", {})
                print(f"🔧 Calling tool: {tool}")
                print(f"   Arguments: {args}")
                tool_calls.append(tool)
            
            elif event.get("type") == "tool_result":
                result = event.get("result", "")
                print(f"✅ Tool result received ({len(result)} chars)")
            
            elif event.get("type") == "task_completed":
                break
        
        result = await task.get_result(timeout=60.0)
        print(f"\n📊 Final Result:")
        print(f"   Tools used: {', '.join(set(tool_calls))}")
        print(f"   Output: {result.output[:300]}...")
        
    except Exception as e:
        print(f"❌ Error: {e}")
    finally:
        await client.close()


async def example_6_multi_agent_dag():
    """Example 6: Multi-agent DAG workflow"""
    print("\n" + "=" * 60)
    print("Example 6: Multi-Agent DAG Workflow")
    print("=" * 60)
    
    client = AsyncShannonClient(
        grpc_endpoint="localhost:50052",
        api_key="test-key"
    )
    
    try:
        # Complex task that will be decomposed into subtasks
        task = await client.submit_task(
            query="""
            Research the following topics in parallel:
            1. Latest developments in AI safety
            2. Recent breakthroughs in quantum computing
            3. Current trends in renewable energy
            
            Then synthesize the findings into a brief report.
            """,
            context={
                "decompose": True,
                "max_agents": 4
            },
            user_id="example-user"
        )
        
        print(f"✅ Complex task submitted: {task.task_id}")
        print("   Will be decomposed into parallel subtasks")
        
        # Track agents
        agents = set()
        async for event in task.stream_events():
            if event.get("type") == "decomposition":
                subtasks = event.get("subtasks", [])
                print(f"\n📋 Task decomposed into {len(subtasks)} subtasks:")
                for i, subtask in enumerate(subtasks, 1):
                    print(f"   {i}. {subtask.get('description', '')[:50]}...")
            
            elif event.get("type") == "agent_started":
                agent = event.get("agent_id")
                agents.add(agent)
                print(f"\n🤖 Agent started: {agent}")
            
            elif event.get("type") == "agent_completed":
                agent = event.get("agent_id")
                print(f"✅ Agent completed: {agent}")
            
            elif event.get("type") == "task_completed":
                print(f"\n✅ All agents completed!")
                break
        
        result = await task.get_result(timeout=120.0)
        print(f"\n📊 DAG Execution Summary:")
        print(f"   Total agents used: {len(agents)}")
        print(f"   Output: {result.output[:300]}...")
        
    except Exception as e:
        print(f"❌ Error: {e}")
    finally:
        await client.close()


async def main():
    """Run all examples"""
    examples = [
        ("Simple Task", example_1_simple_task),
        ("Streaming", example_2_streaming),
        ("Conversational", example_3_with_session),
        ("Template Workflow", example_4_template_workflow),
        ("With Tools", example_5_with_tools),
        ("Multi-Agent DAG", example_6_multi_agent_dag),
    ]
    
    print("\n" + "=" * 60)
    print("Shannon Quick Start Examples")
    print("=" * 60)
    print("\nAvailable examples:")
    for i, (name, _) in enumerate(examples, 1):
        print(f"  {i}. {name}")
    
    print("\nRunning all examples...")
    print("(Press Ctrl+C to skip to next example)")
    
    for name, example_func in examples:
        try:
            await example_func()
            print(f"\n✅ {name} completed")
            await asyncio.sleep(2)  # Pause between examples
        except KeyboardInterrupt:
            print(f"\n⏩ Skipping {name}")
            continue
        except Exception as e:
            print(f"\n❌ {name} failed: {e}")
            continue
    
    print("\n" + "=" * 60)
    print("All examples completed!")
    print("=" * 60)


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        print("\n\nExamples interrupted by user")
        sys.exit(0)

