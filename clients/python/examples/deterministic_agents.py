"""Example of deterministic agents and optional swarm follow-up messaging."""

import json
import os

from shannon import ShannonClient


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")
    selected_agent_id = os.getenv("SHANNON_AGENT_ID")
    raw_agent_input = os.getenv("SHANNON_AGENT_INPUT")
    session_id = os.getenv("SHANNON_SESSION_ID")
    follow_up_message = os.getenv("SHANNON_SWARM_MESSAGE")

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        agents = client.list_agents()
        print(f"Agents: {len(agents)}")
        for agent in agents[:10]:
            print(f"  - {agent.id}: {agent.name} ({agent.tool})")

        if not agents:
            return

        agent_id = selected_agent_id or agents[0].id
        agent = client.get_agent(agent_id)
        print()
        print(f"Agent: {agent.id}")
        print(f"Name: {agent.name}")
        print(f"Description: {agent.description}")
        print(f"Input schema: {json.dumps(agent.input_schema, indent=2, sort_keys=True)}")

        if not raw_agent_input:
            print()
            print("Set SHANNON_AGENT_INPUT to execute this agent.")
            return

        agent_input = json.loads(raw_agent_input)
        execution = client.execute_agent(
            agent_id,
            agent_input,
            session_id=session_id,
        )
        print()
        print(f"Task ID: {execution.task_id}")
        print(f"Workflow ID: {execution.workflow_id}")
        print(f"Status: {execution.status}")

        if follow_up_message:
            result = client.send_swarm_message(execution.workflow_id, follow_up_message)
            print(f"Swarm follow-up accepted: {result.success}")

        final = execution.wait(timeout=120)
        print()
        print(final.result)
    finally:
        client.close()


if __name__ == "__main__":
    main()
