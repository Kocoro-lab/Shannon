#!/usr/bin/env python3
"""
Shannon Cloud OpenAI-Compatible API - LangChain Example

This example demonstrates how to use Shannon Cloud with LangChain
for building AI-powered applications.

Requirements:
    pip install langchain-openai langchain

Usage:
    export SHANNON_API_KEY="sk-shannon-your-api-key"
    python langchain_example.py
"""

import os
from langchain_openai import ChatOpenAI
from langchain_core.messages import HumanMessage, SystemMessage
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import StrOutputParser

# Configuration
API_KEY = os.getenv("SHANNON_API_KEY", "sk-shannon-your-api-key")
BASE_URL = os.getenv("SHANNON_BASE_URL", "https://api.shannon.run/v1")


def create_llm(model: str = "shannon-chat", streaming: bool = False):
    """Create a LangChain ChatOpenAI instance configured for Shannon Cloud."""
    return ChatOpenAI(
        model=model,
        api_key=API_KEY,
        base_url=BASE_URL,
        streaming=streaming
    )


def simple_invoke():
    """Simple LLM invocation."""
    print("Simple Invoke Example")
    print("-" * 40)

    llm = create_llm()
    response = llm.invoke("What is machine learning in one sentence?")
    print(f"Response: {response.content}\n")


def with_messages():
    """Using message objects."""
    print("Messages Example")
    print("-" * 40)

    llm = create_llm()
    messages = [
        SystemMessage(content="You are a helpful coding assistant."),
        HumanMessage(content="Write a Python function to check if a number is prime.")
    ]
    response = llm.invoke(messages)
    print(f"Response:\n{response.content}\n")


def with_prompt_template():
    """Using prompt templates."""
    print("Prompt Template Example")
    print("-" * 40)

    llm = create_llm()

    prompt = ChatPromptTemplate.from_messages([
        ("system", "You are a {role} expert."),
        ("human", "Explain {topic} in simple terms.")
    ])

    chain = prompt | llm | StrOutputParser()
    response = chain.invoke({"role": "data science", "topic": "neural networks"})
    print(f"Response: {response}\n")


def streaming_response():
    """Streaming responses with LangChain."""
    print("Streaming Example")
    print("-" * 40)

    llm = create_llm(streaming=True)

    print("Response: ", end="")
    for chunk in llm.stream("Tell me a short joke about programming."):
        print(chunk.content, end="", flush=True)
    print("\n")


def research_chain():
    """Research chain with Shannon's deep research model."""
    print("Research Chain Example")
    print("-" * 40)

    # Use deep research model
    llm = create_llm(model="shannon-deep-research", streaming=True)

    prompt = ChatPromptTemplate.from_messages([
        ("system", "You are a market research analyst. Provide detailed, well-sourced analysis."),
        ("human", "Research: {query}")
    ])

    chain = prompt | llm | StrOutputParser()

    print("Researching... (this may take a minute)\n")
    for chunk in chain.stream({"query": "What are the top 3 trends in SaaS pricing for 2024?"}):
        print(chunk, end="", flush=True)
    print("\n")


def multi_step_chain():
    """Multi-step chain example."""
    print("Multi-Step Chain Example")
    print("-" * 40)

    # Step 1: Quick research to gather facts
    research_llm = create_llm(model="shannon-quick-research")

    # Step 2: Chat model to synthesize
    chat_llm = create_llm(model="shannon-chat")

    # Research prompt
    research_prompt = ChatPromptTemplate.from_messages([
        ("system", "You are a research assistant. Gather key facts about the topic."),
        ("human", "Research: {topic}")
    ])

    # Synthesis prompt
    synthesis_prompt = ChatPromptTemplate.from_messages([
        ("system", "You are a writer. Create a brief executive summary from the research."),
        ("human", "Research findings:\n{research}\n\nCreate a 2-3 sentence executive summary.")
    ])

    # Build chains
    research_chain = research_prompt | research_llm | StrOutputParser()
    synthesis_chain = synthesis_prompt | chat_llm | StrOutputParser()

    # Execute
    topic = "electric vehicle adoption trends"
    print(f"Topic: {topic}\n")

    print("Step 1: Researching...")
    research_result = research_chain.invoke({"topic": topic})
    print(f"Research complete ({len(research_result)} chars)\n")

    print("Step 2: Synthesizing...")
    summary = synthesis_chain.invoke({"research": research_result[:2000]})  # Truncate for demo
    print(f"Summary: {summary}\n")


def main():
    print("=" * 60)
    print("Shannon Cloud OpenAI-Compatible API - LangChain Examples")
    print("=" * 60)
    print()

    try:
        # Basic examples
        simple_invoke()
        with_messages()
        with_prompt_template()
        streaming_response()

        # Uncomment for longer examples:
        # research_chain()
        # multi_step_chain()

    except Exception as e:
        print(f"Error: {e}")
        print("\nMake sure to set SHANNON_API_KEY environment variable.")


if __name__ == "__main__":
    main()
