#!/usr/bin/env python3
"""
Convert tctl workflow show output to JSON format.
This is a fallback tool for environments where temporal CLI is not available.

Usage:
    tctl workflow show --workflow_id <id> | python3 tctl_to_json.py > history.json
"""

import sys
import json
import re
from typing import Dict, Any, List

def parse_tctl_output(input_text: str) -> Dict[str, Any]:
    """
    Parse tctl workflow show output and convert to JSON format compatible with Temporal replay.
    """
    # Find the start of JSON data (after the header info)
    lines = input_text.strip().split('\n')
    
    # Look for the pattern that indicates start of event data
    json_start = -1
    for i, line in enumerate(lines):
        # tctl outputs events starting with "{EventId:" or similar JSON-like structure
        if line.strip().startswith('{') and 'EventId' in line:
            json_start = i
            break
    
    if json_start == -1:
        # Maybe it's already JSON?
        try:
            return json.loads(input_text)
        except:
            raise ValueError("Could not find event data in tctl output")
    
    # Extract events
    events = []
    for line in lines[json_start:]:
        line = line.strip()
        if not line or not line.startswith('{'):
            continue
            
        # Convert tctl's pseudo-JSON to actual JSON
        # tctl outputs like: {EventId:1, EventTime:2025-01-01 00:00:00 +0000 UTC, ...}
        # We need: {"eventId": "1", "eventTime": "2025-01-01T00:00:00Z", ...}
        
        # This is a simplified parser - in production you'd want something more robust
        try:
            # Replace field names to match JSON format
            json_line = line
            json_line = re.sub(r'EventId:(\d+)', r'"eventId": "\1"', json_line)
            json_line = re.sub(r'EventTime:([^,}]+)', r'"eventTime": "\1"', json_line)
            json_line = re.sub(r'EventType:([^,}]+)', r'"eventType": "\1"', json_line)
            json_line = re.sub(r'TaskId:(\d+)', r'"taskId": "\1"', json_line)
            json_line = re.sub(r'Version:(\d+)', r'"version": "\1"', json_line)
            
            # Parse and add to events
            event = json.loads(json_line)
            events.append(event)
        except Exception as e:
            print(f"Warning: Could not parse event line: {line}", file=sys.stderr)
            print(f"Error: {e}", file=sys.stderr)
            continue
    
    if not events:
        raise ValueError("No events could be parsed from tctl output")
    
    # Return in Temporal's expected format
    return {
        "events": events,
        "namespace": "default"
    }

def main():
    """Read tctl output from stdin and output JSON to stdout."""
    try:
        input_text = sys.stdin.read()
        result = parse_tctl_output(input_text)
        print(json.dumps(result, indent=2))
    except Exception as e:
        print(f"Error converting tctl output: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()