#!/usr/bin/env python3
"""
Mock Python LLM service for testing Rust-Python contract.
This simulates the expected API endpoints for integration testing.
"""

from flask import Flask, request, jsonify
import logging
import sys

app = Flask(__name__)
logging.basicConfig(level=logging.INFO)

@app.route('/health', methods=['GET'])
def health():
    """Health check endpoint"""
    return jsonify({
        "status": "healthy",
        "service": "mock-llm-service",
        "version": "1.0.0"
    })

@app.route('/tools/list', methods=['GET'])
def list_tools():
    """List available tools"""
    exclude_dangerous = request.args.get('exclude_dangerous', 'false').lower() == 'true'
    
    tools = [
        "calculator",
        "web_search",
        "database_query",
    ]
    
    if not exclude_dangerous:
        tools.append("code_executor")
    
    return jsonify(tools)

@app.route('/tools/select', methods=['POST'])
def select_tools():
    """Select appropriate tools for a task"""
    data = request.json
    task = data.get('task', '')
    exclude_dangerous = data.get('exclude_dangerous', False)
    
    selected = []
    
    # Simple heuristic selection
    if 'calculate' in task.lower() or 'math' in task.lower() or '+' in task or '-' in task:
        selected.append({
            "tool_name": "calculator",
            "parameters": {"expression": task}
        })
    
    if 'search' in task.lower() or 'find' in task.lower():
        selected.append({
            "tool_name": "web_search", 
            "parameters": {"query": task, "max_results": 5}
        })
    
    if 'code' in task.lower() and not exclude_dangerous:
        selected.append({
            "tool_name": "code_executor",
            "parameters": {}
        })
    
    return jsonify({
        "calls": selected,
        "provider_used": "mock"
    })

@app.route('/tools/execute', methods=['POST'])
def execute_tool():
    """Execute a tool"""
    data = request.json
    tool_name = data.get('tool_name')
    parameters = data.get('parameters', {})
    
    if tool_name == 'calculator':
        expression = parameters.get('expression', '0')
        try:
            # Simple evaluation (unsafe in production!)
            result = eval(expression.replace('^', '**'))
            return jsonify({
                "success": True,
                "output": result,
                "error": None
            })
        except Exception as e:
            return jsonify({
                "success": False,
                "output": None,
                "error": str(e)
            })
    
    elif tool_name == 'web_search':
        query = parameters.get('query', '')
        max_results = parameters.get('max_results', 3)
        
        # Mock search results
        results = []
        for i in range(min(max_results, 3)):
            results.append({
                "title": f"Result {i+1} for: {query}",
                "url": f"https://example.com/result{i+1}",
                "snippet": f"Mock search result {i+1} containing information about {query}"
            })
        
        return jsonify({
            "success": True,
            "output": results,
            "error": None
        })
    
    elif tool_name == 'code_executor':
        return jsonify({
            "success": False,
            "output": None,
            "error": "Code execution not implemented in mock"
        })
    
    else:
        return jsonify({
            "success": False,
            "output": None,
            "error": f"Unknown tool: {tool_name}"
        })

@app.route('/analyze_task', methods=['POST'])
@app.route('/complexity/analyze_task', methods=['POST'])  # Support both paths
def analyze_task():
    """Analyze task complexity"""
    data = request.json
    query = data.get('query', '')
    
    # Simple heuristic for complexity
    word_count = len(query.split())
    
    if word_count < 5:
        mode = "simple"
        subtasks = []
    elif word_count < 15:
        mode = "standard"
        subtasks = ["analyze", "execute", "verify"]
    else:
        mode = "complex"
        subtasks = [
            "break down requirements",
            "design solution",
            "implement components",
            "integrate",
            "test"
        ]
    
    return jsonify({
        "execution_mode": mode,
        "subtasks": subtasks,
        "estimated_tokens": word_count * 10,
        "confidence": 0.8
    })

if __name__ == '__main__':
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8000
    print(f"Starting mock Python LLM service on port {port}")
    print(f"Use this for testing: LLM_SERVICE_URL=http://localhost:{port} cargo test")
    app.run(host='0.0.0.0', port=port, debug=True)