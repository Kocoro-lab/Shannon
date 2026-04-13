"""Example of the OpenAI-compatible helpers in the Shannon SDK."""

import os

from shannon import (
    OpenAIChatMessage,
    OpenAIShannonOptions,
    ShannonClient,
)


def main() -> None:
    base_url = os.getenv("SHANNON_BASE_URL", "http://localhost:8080")
    api_key = os.getenv("SHANNON_API_KEY")
    model = os.getenv("SHANNON_OPENAI_MODEL", "shannon-chat")
    prompt = os.getenv("SHANNON_OPENAI_PROMPT", "Summarize Shannon's strengths in two sentences.")
    stream = os.getenv("SHANNON_STREAM", "0") == "1"

    client = ShannonClient(base_url=base_url, api_key=api_key)
    try:
        models = client.list_openai_models()
        print(f"OpenAI-compatible models: {len(models)}")
        for entry in models[:10]:
            print(f"  - {entry.id}")

        messages = [OpenAIChatMessage(role="user", content=prompt)]
        options = OpenAIShannonOptions(research_strategy="standard")

        if stream:
            print()
            print("Streaming response:")
            for chunk in client.stream_chat_completion(
                messages,
                model=model,
                include_usage=True,
                shannon_options=options,
            ):
                if chunk.choices and chunk.choices[0].delta and chunk.choices[0].delta.content:
                    print(chunk.choices[0].delta.content, end="", flush=True)
            print()
            return

        completion = client.create_chat_completion(
            messages,
            model=model,
            shannon_options=options,
        )
        print()
        print(completion.choices[0].message.content)
        if completion.usage:
            print(f"Tokens: {completion.usage.total_tokens}")
    finally:
        client.close()


if __name__ == "__main__":
    main()
