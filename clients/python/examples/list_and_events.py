"""Example showing list_tasks() and get_task_events()."""

import os
from shannon import ShannonClient


def main():
    client = ShannonClient(
        base_url="http://localhost:8080",
        api_key=os.getenv("SHANNON_API_KEY", ""),
    )

    try:
        print("Listing recent tasks (limit=5)...")
        tasks, total = client.list_tasks(limit=5)
        print(f"Total tasks: {total}, showing {len(tasks)}")
        for t in tasks:
            created = t.created_at.isoformat()
            print(f"  - {t.task_id} [{t.status}] @ {created}: {t.query[:60]}")

        if tasks:
            first = tasks[0]
            print(f"\nFetching events for task {first.task_id}...")
            events = client.get_task_events(first.task_id)
            print(f"Found {len(events)} events; showing first 5:")
            for e in events[:5]:
                print(f"  * {e.timestamp.isoformat()} {e.type}: {e.message[:80]}")
        else:
            print("No tasks yet; submit one with the 'submit' CLI command.")

    finally:
        client.close()


if __name__ == "__main__":
    main()

