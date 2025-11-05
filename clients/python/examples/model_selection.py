"""Example: model selection parameters in ShannonClient.

Run:
  SHANNON_BASE_URL=http://localhost:8080 \
  SHANNON_API_KEY=sk_xxx \
  python clients/python/examples/model_selection.py
"""

import os
from shannon import ShannonClient


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        handle = client.submit_task(
            "Summarize this sentence: William Shakespeare was an English playwright, poet and actor. He is widely regarded as the greatest writer in the English language and the world's pre-eminent dramatist.",
            # Choose by tier, or override explicitly below
            model_tier="small",
            mode="simple",  # simple | standard | complex | supervisor
            # model_override="gpt-5-nano-2025-08-07",
            # provider_override="openai",
        )

        final = client.wait(handle.task_id, timeout=60)
        print(f"Task: {handle.task_id}")
        print(f"Result: {final.result}")
    finally:
        client.close()


if __name__ == "__main__":
    main()

