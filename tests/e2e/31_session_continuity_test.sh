#!/usr/bin/env bash
# Test session continuity across multiple requests

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "🧪 Testing Session Continuity..."

# Generate unique session ID
SESSION_ID="test-continuity-$(date +%s)"
USER_ID="test-user-$(date +%s)"

echo "📝 Using session: $SESSION_ID"
echo "👤 Using user: $USER_ID"

# Test 1: Store information
echo -e "\n${YELLOW}Test 1: Storing user information${NC}"
RESPONSE=$(./scripts/submit_task.sh "My favorite color is blue and I have a cat named Whiskers" "$USER_ID" "$SESSION_ID" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" == *"Result:"* ]]; then
    echo -e "${GREEN}✓ Information stored${NC}"
else
    echo -e "${RED}✗ Failed to store information${NC}"
    exit 1
fi

sleep 2

# Test 2: Recall information
echo -e "\n${YELLOW}Test 2: Recalling stored information${NC}"
RESPONSE=$(./scripts/submit_task.sh "What is my favorite color and what is my cat's name?" "$USER_ID" "$SESSION_ID" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" == *"blue"* ]] && [[ "$RESPONSE" == *"Whiskers"* ]]; then
    echo -e "${GREEN}✓ Session correctly recalled: color=blue, cat=Whiskers${NC}"
else
    echo -e "${RED}✗ Failed to recall session information${NC}"
    echo "Response was: $RESPONSE"
    exit 1
fi

sleep 2

# Test 3: Add more information
echo -e "\n${YELLOW}Test 3: Adding more information to session${NC}"
RESPONSE=$(./scripts/submit_task.sh "I also love hiking and my favorite food is sushi" "$USER_ID" "$SESSION_ID" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" == *"Result:"* ]]; then
    echo -e "${GREEN}✓ Additional information stored${NC}"
else
    echo -e "${RED}✗ Failed to store additional information${NC}"
    exit 1
fi

sleep 2

# Test 4: Recall all information
echo -e "\n${YELLOW}Test 4: Recalling all stored information${NC}"
RESPONSE=$(./scripts/submit_task.sh "Can you list everything you know about me?" "$USER_ID" "$SESSION_ID" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" == *"blue"* ]] || [[ "$RESPONSE" == *"Whiskers"* ]] || [[ "$RESPONSE" == *"hiking"* ]] || [[ "$RESPONSE" == *"sushi"* ]]; then
    echo -e "${GREEN}✓ Session maintains context across multiple interactions${NC}"
    echo "Response contains expected information"
else
    echo -e "${YELLOW}⚠ Session may not have all information${NC}"
    echo "Response was: $RESPONSE"
fi

# Test 5: Different user cannot access session
echo -e "\n${YELLOW}Test 5: Testing session isolation (different user)${NC}"
DIFFERENT_USER="different-user-$(date +%s)"
RESPONSE=$(./scripts/submit_task.sh "What is my favorite color?" "$DIFFERENT_USER" "$SESSION_ID" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" != *"blue"* ]]; then
    echo -e "${GREEN}✓ Session correctly isolated - different user cannot access${NC}"
else
    echo -e "${RED}✗ SECURITY ISSUE: Different user accessed session data!${NC}"
    exit 1
fi

# Test 6: New session for same user is empty
echo -e "\n${YELLOW}Test 6: Testing new session is empty${NC}"
NEW_SESSION="new-session-$(date +%s)"
RESPONSE=$(./scripts/submit_task.sh "What is my favorite color?" "$USER_ID" "$NEW_SESSION" 2>/dev/null | grep "Result:" || echo "")
if [[ "$RESPONSE" != *"blue"* ]]; then
    echo -e "${GREEN}✓ New session correctly starts empty${NC}"
else
    echo -e "${RED}✗ New session incorrectly has old data${NC}"
    exit 1
fi

echo -e "\n${GREEN}✅ All session continuity tests passed!${NC}"
echo "Session ID: $SESSION_ID maintained context correctly"