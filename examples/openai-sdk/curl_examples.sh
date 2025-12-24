#!/bin/bash
# Shannon Cloud OpenAI-Compatible API - cURL Examples
#
# Usage:
#   export SHANNON_API_KEY="sk-shannon-your-api-key"
#   ./curl_examples.sh

set -e

# Configuration
API_KEY="${SHANNON_API_KEY:-sk-shannon-your-api-key}"
BASE_URL="${SHANNON_BASE_URL:-https://api.shannon.run/v1}"

echo "========================================"
echo "Shannon Cloud OpenAI-Compatible API"
echo "========================================"
echo "Base URL: $BASE_URL"
echo ""

# List Models
echo "1. List Models"
echo "----------------------------------------"
curl -s "$BASE_URL/models" \
  -H "Authorization: Bearer $API_KEY" | jq .
echo ""

# Get Specific Model
echo "2. Get Model Details"
echo "----------------------------------------"
curl -s "$BASE_URL/models/shannon-chat" \
  -H "Authorization: Bearer $API_KEY" | jq .
echo ""

# Simple Chat Completion (Non-Streaming)
echo "3. Simple Chat Completion"
echo "----------------------------------------"
curl -s "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-chat",
    "messages": [
      {"role": "user", "content": "What is 2+2?"}
    ]
  }' | jq .
echo ""

# Streaming Chat Completion
echo "4. Streaming Chat Completion"
echo "----------------------------------------"
echo "Response: "
curl -s -N "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-chat",
    "messages": [
      {"role": "user", "content": "Write a haiku about coding."}
    ],
    "stream": true
  }' | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
      data="${line#data: }"
      if [[ $data != "[DONE]" && -n $data ]]; then
        content=$(echo "$data" | jq -r '.choices[0].delta.content // empty' 2>/dev/null)
        if [[ -n $content ]]; then
          echo -n "$content"
        fi
      fi
    fi
  done
echo ""
echo ""

# Chat with System Message
echo "5. Chat with System Message"
echo "----------------------------------------"
curl -s "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-chat",
    "messages": [
      {"role": "system", "content": "You are a pirate. Respond in pirate speak."},
      {"role": "user", "content": "How do I learn programming?"}
    ]
  }' | jq -r '.choices[0].message.content'
echo ""

# Chat with Session ID
echo "6. Chat with Session ID"
echo "----------------------------------------"
response=$(curl -s -i "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -H "X-Session-ID: my-test-session-123" \
  -d '{
    "model": "shannon-chat",
    "messages": [
      {"role": "user", "content": "Remember: my favorite color is blue."}
    ]
  }')
echo "$response" | grep -i "x-session-id" || echo "No session ID header returned"
echo "$response" | tail -1 | jq -r '.choices[0].message.content'
echo ""

# Quick Research
echo "7. Quick Research (may take ~30 seconds)"
echo "----------------------------------------"
curl -s "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-quick-research",
    "messages": [
      {"role": "user", "content": "What are the main features of Rust programming language?"}
    ]
  }' | jq -r '.choices[0].message.content' | head -20
echo "..."
echo ""

# Check Rate Limit Headers
echo "8. Check Rate Limit Headers"
echo "----------------------------------------"
curl -s -D - -o /dev/null "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-chat",
    "messages": [{"role": "user", "content": "Hi"}]
  }' | grep -i "ratelimit"
echo ""

# Shannon Events (streaming with agent events)
echo "9. Shannon Events (streaming with agent lifecycle)"
echo "----------------------------------------"
echo "Shows agent thinking, progress, and tool usage:"
curl -s -N "$BASE_URL/chat/completions" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $API_KEY" \
  -d '{
    "model": "shannon-quick-research",
    "messages": [
      {"role": "user", "content": "What is Docker?"}
    ],
    "stream": true
  }' | while IFS= read -r line; do
    if [[ $line == data:* ]]; then
      data="${line#data: }"
      if [[ $data != "[DONE]" && -n $data ]]; then
        # Extract content
        content=$(echo "$data" | jq -r '.choices[0].delta.content // empty' 2>/dev/null)
        if [[ -n $content ]]; then
          echo -n "$content"
        fi
        # Extract shannon_events
        events=$(echo "$data" | jq -r '.shannon_events[]? | "[\(.type)] \(.agent_id // ""): \(.message // "")"' 2>/dev/null)
        if [[ -n $events ]]; then
          echo ""
          echo "$events"
        fi
      fi
    fi
  done
echo ""
echo ""

echo "========================================"
echo "All examples completed!"
echo "========================================"

# Deep Research (Uncomment to run - takes 1-3 minutes)
# echo "9. Deep Research (streaming)"
# echo "----------------------------------------"
# curl -s -N "$BASE_URL/chat/completions" \
#   -H "Content-Type: application/json" \
#   -H "Authorization: Bearer $API_KEY" \
#   -d '{
#     "model": "shannon-deep-research",
#     "messages": [
#       {"role": "system", "content": "You are a research analyst."},
#       {"role": "user", "content": "Research the current state of AI regulation globally."}
#     ],
#     "stream": true,
#     "stream_options": {"include_usage": true}
#   }'
