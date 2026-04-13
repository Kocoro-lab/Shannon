"""Example of workspace and memory file access."""

import os

from shannon import ShannonClient


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")
    session_id = os.getenv("SHANNON_SESSION_ID")
    memory_file = os.getenv("SHANNON_MEMORY_FILE")
    session_file = os.getenv("SHANNON_SESSION_FILE")

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        memory_files = client.list_memory_files()
        print(f"Memory files: {len(memory_files)}")
        for entry in memory_files[:10]:
            print(f"  - {entry.path} ({entry.size_bytes} bytes)")

        if memory_file:
            downloaded = client.download_memory_file(memory_file)
            print()
            print(f"Downloaded memory file: {memory_file}")
            print(downloaded.content[:400])

        if not session_id:
            print()
            print("Set SHANNON_SESSION_ID to inspect workspace files for a session.")
            return

        session_files = client.list_session_files(session_id)
        print()
        print(f"Session files for {session_id}: {len(session_files)}")
        for entry in session_files[:10]:
            print(f"  - {entry.path} ({entry.size_bytes} bytes)")

        if session_file:
            downloaded = client.download_session_file(session_id, session_file)
            print()
            print(f"Downloaded session file: {session_file}")
            print(downloaded.content[:400])
    finally:
        client.close()


if __name__ == "__main__":
    main()
