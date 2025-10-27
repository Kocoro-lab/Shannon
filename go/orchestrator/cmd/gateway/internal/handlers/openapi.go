package handlers

import (
	"encoding/json"
	"net/http"
)

// OpenAPIHandler serves the OpenAPI specification
type OpenAPIHandler struct {
	spec map[string]interface{}
}

// NewOpenAPIHandler creates a new OpenAPI handler
func NewOpenAPIHandler() *OpenAPIHandler {
	return &OpenAPIHandler{
		spec: generateOpenAPISpec(),
	}
}

// ServeSpec handles GET /openapi.json
func (h *OpenAPIHandler) ServeSpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(h.spec)
}

// generateOpenAPISpec creates the OpenAPI 3.0 specification
func generateOpenAPISpec() map[string]interface{} {
	return map[string]interface{}{
		"openapi": "3.0.0",
		"info": map[string]interface{}{
			"title":       "Shannon Gateway API",
			"version":     "0.1.0",
			"description": "REST API for Shannon multi-agent AI platform",
		},
		"servers": []map[string]interface{}{
			{
				"url":         "http://localhost:8080",
				"description": "Local development server",
			},
			{
				"url":         "https://api.shannon.ai",
				"description": "Production server",
			},
		},
		"security": []map[string]interface{}{
			{"apiKey": []string{}},
		},
		"paths": map[string]interface{}{
			"/health": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Health check",
					"description": "Check if the gateway is healthy",
					"security":    []interface{}{},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Gateway is healthy",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/HealthResponse",
									},
								},
							},
						},
					},
				},
			},
			"/api/v1/tasks": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List tasks",
					"description": "List tasks for the authenticated user (optionally filter by session/status)",
					"parameters": []map[string]interface{}{
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 20, "minimum": 1, "maximum": 100}},
						{"name": "offset", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 0, "minimum": 0}},
						{"name": "status", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"QUEUED", "RUNNING", "COMPLETED", "FAILED", "CANCELLED", "TIMEOUT"}}},
						{"name": "session_id", "in": "query", "schema": map[string]interface{}{"type": "string"}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "List of tasks",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ListTasksResponse",
									},
								},
							},
						},
					},
				},
				"post": map[string]interface{}{
					"summary":     "Submit a task",
					"description": "Submit a new task for processing",
					"parameters": []map[string]interface{}{
						{
							"name":        "Idempotency-Key",
							"in":          "header",
							"description": "Unique key for idempotent requests",
							"required":    false,
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/TaskRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Task submitted successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/TaskResponse",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"429": map[string]interface{}{
							"description": "Rate limit exceeded",
						},
					},
				},
			},
			"/api/v1/tasks/stream": map[string]interface{}{
				"post": map[string]interface{}{
					"summary":     "Submit task and get stream URL",
					"description": "Submit a task and immediately receive the workflow ID and SSE stream URL for real-time updates. This is a convenience endpoint that combines task submission with stream URL generation in a single call.",
					"parameters": []map[string]interface{}{
						{
							"name":        "Idempotency-Key",
							"in":          "header",
							"description": "Unique key for idempotent requests",
							"required":    false,
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"requestBody": map[string]interface{}{
						"required": true,
						"content": map[string]interface{}{
							"application/json": map[string]interface{}{
								"schema": map[string]interface{}{
									"$ref": "#/components/schemas/TaskRequest",
								},
							},
						},
					},
					"responses": map[string]interface{}{
						"201": map[string]interface{}{
							"description": "Task submitted successfully with stream URL",
							"headers": map[string]interface{}{
								"X-Workflow-ID": map[string]interface{}{
									"description": "The workflow ID for this task",
									"schema": map[string]interface{}{
										"type": "string",
									},
								},
								"X-Session-ID": map[string]interface{}{
									"description": "The session ID associated with this task",
									"schema": map[string]interface{}{
										"type": "string",
									},
								},
								"Link": map[string]interface{}{
									"description": "Link header with stream URL (rel=stream)",
									"schema": map[string]interface{}{
										"type": "string",
									},
								},
							},
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/TaskStreamResponse",
									},
								},
							},
						},
						"400": map[string]interface{}{
							"description": "Invalid request",
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"429": map[string]interface{}{
							"description": "Rate limit exceeded",
						},
					},
				},
			},
			"/api/v1/tasks/{id}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get task status",
					"description": "Get the status of a submitted task",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"description": "Task ID",
							"required":    true,
							"schema": map[string]interface{}{
								"type":      "string",
								"minLength": 1,
								"maxLength": 128,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Task status retrieved",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/TaskStatusResponse",
									},
								},
							},
						},
						"404": map[string]interface{}{
							"description": "Task not found",
						},
					},
				},
			},
			"/api/v1/tasks/{id}/stream": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Stream task events",
					"description": "Stream real-time events for a task via Server-Sent Events",
					"parameters": []map[string]interface{}{
						{
							"name":        "id",
							"in":          "path",
							"description": "Task ID",
							"required":    true,
							"schema": map[string]interface{}{
								"type":      "string",
								"minLength": 1,
								"maxLength": 128,
							},
						},
						{
							"name":        "types",
							"in":          "query",
							"description": "Comma-separated list of event types to filter",
							"required":    false,
							"schema": map[string]interface{}{
								"type": "string",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Event stream established",
							"content": map[string]interface{}{
								"text/event-stream": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "string",
									},
								},
							},
						},
					},
				},
			},
			"/api/v1/stream/sse": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "SSE stream",
					"description": "Server-Sent Events stream for workflow events",
					"parameters": []map[string]interface{}{
						{
							"name":        "workflow_id",
							"in":          "query",
							"description": "Workflow ID to stream events for",
							"required":    true,
							"schema": map[string]interface{}{
								"type":      "string",
								"minLength": 1,
								"maxLength": 128,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Event stream",
						},
					},
				},
			},
			"/api/v1/tasks/{id}/events": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get task events",
					"description": "Get historical events for a task",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string", "minLength": 1, "maxLength": 128}},
						{"name": "limit", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 50, "minimum": 1, "maximum": 200}},
						{"name": "offset", "in": "query", "schema": map[string]interface{}{"type": "integer", "default": 0, "minimum": 0}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Task events",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"type": "object",
										"properties": map[string]interface{}{
											"events": map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/TaskEvent"}},
											"count":  map[string]interface{}{"type": "integer"},
										},
									},
								},
							},
						},
					},
				},
			},
			"/api/v1/tasks/{id}/timeline": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Build timeline from Temporal history",
					"description": "Derive a human-readable timeline from Temporal history; optionally persist asynchronously",
					"parameters": []map[string]interface{}{
						{"name": "id", "in": "path", "required": true, "schema": map[string]interface{}{"type": "string"}},
						{"name": "run_id", "in": "query", "schema": map[string]interface{}{"type": "string"}},
						{"name": "mode", "in": "query", "schema": map[string]interface{}{"type": "string", "enum": []string{"summary", "full"}, "default": "summary"}},
						{"name": "include_payloads", "in": "query", "schema": map[string]interface{}{"type": "boolean", "default": false}},
						{"name": "persist", "in": "query", "schema": map[string]interface{}{"type": "boolean", "default": true}},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{"description": "Timeline returned inline"},
						"202": map[string]interface{}{"description": "Accepted for async persistence"},
					},
				},
			},
			"/api/v1/sessions": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "List all sessions",
					"description": "Retrieve all sessions for the authenticated user with pagination support",
					"parameters": []map[string]interface{}{
						{
							"name":        "limit",
							"in":          "query",
							"description": "Maximum number of sessions to return",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 20,
								"minimum": 1,
								"maximum": 100,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"description": "Number of sessions to skip",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Sessions retrieved successfully",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/ListSessionsResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
					},
				},
			},
			"/api/v1/sessions/{sessionId}": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get session metadata",
					"description": "Retrieve session information including token usage and task count",
					"parameters": []map[string]interface{}{
						{
							"name":        "sessionId",
							"in":          "path",
							"description": "Session UUID",
							"required":    true,
							"schema": map[string]interface{}{
								"type":   "string",
								"format": "uuid",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Session metadata retrieved",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/SessionResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Forbidden - User does not have access to this session",
						},
						"404": map[string]interface{}{
							"description": "Session not found",
						},
					},
				},
			},
			"/api/v1/sessions/{sessionId}/history": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get session task history",
					"description": "Retrieve all tasks in a session with full execution details",
					"parameters": []map[string]interface{}{
						{
							"name":        "sessionId",
							"in":          "path",
							"description": "Session UUID",
							"required":    true,
							"schema": map[string]interface{}{
								"type":   "string",
								"format": "uuid",
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Session task history retrieved",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/SessionHistoryResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Forbidden - User does not have access to this session",
						},
						"404": map[string]interface{}{
							"description": "Session not found",
						},
					},
				},
			},
			"/api/v1/sessions/{sessionId}/events": map[string]interface{}{
				"get": map[string]interface{}{
					"summary":     "Get session events (chat history)",
					"description": "Retrieve SSE-like events across all tasks in a session. Excludes LLM_PARTIAL events for cleaner chat history. Supports pagination.",
					"parameters": []map[string]interface{}{
						{
							"name":        "sessionId",
							"in":          "path",
							"description": "Session UUID",
							"required":    true,
							"schema": map[string]interface{}{
								"type":   "string",
								"format": "uuid",
							},
						},
						{
							"name":        "limit",
							"in":          "query",
							"description": "Maximum number of events to return",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 200,
								"minimum": 1,
								"maximum": 500,
							},
						},
						{
							"name":        "offset",
							"in":          "query",
							"description": "Number of events to skip",
							"schema": map[string]interface{}{
								"type":    "integer",
								"default": 0,
								"minimum": 0,
							},
						},
					},
					"responses": map[string]interface{}{
						"200": map[string]interface{}{
							"description": "Session events retrieved",
							"content": map[string]interface{}{
								"application/json": map[string]interface{}{
									"schema": map[string]interface{}{
										"$ref": "#/components/schemas/SessionEventsResponse",
									},
								},
							},
						},
						"401": map[string]interface{}{
							"description": "Unauthorized",
						},
						"403": map[string]interface{}{
							"description": "Forbidden - User does not have access to this session",
						},
						"404": map[string]interface{}{
							"description": "Session not found",
						},
					},
				},
			},
		},
		"components": map[string]interface{}{
			"securitySchemes": map[string]interface{}{
				"apiKey": map[string]interface{}{
					"type":        "apiKey",
					"in":          "header",
					"name":        "X-API-Key",
					"description": "API key for authentication",
				},
			},
			"schemas": map[string]interface{}{
				"HealthResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"version": map[string]interface{}{
							"type": "string",
						},
						"time": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
						"checks": map[string]interface{}{
							"type": "object",
							"additionalProperties": map[string]interface{}{
								"type": "string",
							},
						},
					},
				},
				"TaskRequest": map[string]interface{}{
					"type":     "object",
					"required": []string{"query"},
					"properties": map[string]interface{}{
						"query": map[string]interface{}{
							"type":        "string",
							"description": "The task query or command",
						},
						"context": map[string]interface{}{
							"type":        "object",
							"description": "Additional context for the task",
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"enum":        []string{"simple", "supervisor"},
							"description": "Execution mode",
							"default":     "simple",
						},
					},
				},
				"TaskResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task_id": map[string]interface{}{
							"type": "string",
						},
						"message": map[string]interface{}{
							"type": "string",
						},
						"created_at": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
					},
				},
				"TaskStreamResponse": map[string]interface{}{
					"type": "object",
					"required": []string{"workflow_id", "task_id", "stream_url"},
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{
							"type":        "string",
							"description": "Unique workflow identifier for this task",
							"example":     "task-user123-1234567890",
						},
						"task_id": map[string]interface{}{
							"type":        "string",
							"description": "Task identifier (same as workflow_id)",
							"example":     "task-user123-1234567890",
						},
						"stream_url": map[string]interface{}{
							"type":        "string",
							"description": "SSE endpoint URL to stream real-time events for this task",
							"example":     "/api/v1/stream/sse?workflow_id=task-user123-1234567890",
						},
					},
				},
				"TaskStatusResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task_id": map[string]interface{}{
							"type": "string",
						},
						"response": map[string]interface{}{
							"type": "object",
						},
						"error": map[string]interface{}{
							"type": "string",
						},
						"query": map[string]interface{}{
							"type": "string",
						},
						"mode": map[string]interface{}{
							"type": "string",
						},
						"created_at": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
						"updated_at": map[string]interface{}{
							"type":   "string",
							"format": "date-time",
						},
					},
				},
				"TaskSummary": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task_id":           map[string]interface{}{"type": "string"},
						"query":             map[string]interface{}{"type": "string"},
						"status":            map[string]interface{}{"type": "string"},
						"mode":              map[string]interface{}{"type": "string"},
						"created_at":        map[string]interface{}{"type": "string", "format": "date-time"},
						"completed_at":      map[string]interface{}{"type": "string", "format": "date-time"},
						"total_token_usage": map[string]interface{}{"type": "object"},
					},
				},
				"ListTasksResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"tasks":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"$ref": "#/components/schemas/TaskSummary"}},
						"total_count": map[string]interface{}{"type": "integer"},
					},
				},
				"TaskEvent": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"workflow_id": map[string]interface{}{"type": "string"},
						"type":        map[string]interface{}{"type": "string"},
						"agent_id":    map[string]interface{}{"type": "string"},
						"message":     map[string]interface{}{"type": "string"},
						"timestamp":   map[string]interface{}{"type": "string", "format": "date-time"},
						"seq":         map[string]interface{}{"type": "integer"},
						"stream_id":   map[string]interface{}{"type": "string"},
					},
				},
				"ListSessionsResponse": map[string]interface{}{
					"type": "object",
					"required": []string{"sessions", "total_count"},
					"properties": map[string]interface{}{
						"sessions": map[string]interface{}{
							"type":        "array",
							"description": "List of sessions",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/SessionSummary",
							},
						},
						"total_count": map[string]interface{}{
							"type":        "integer",
							"description": "Total number of sessions for the user",
						},
					},
				},
				"SessionSummary": map[string]interface{}{
					"type":     "object",
					"required": []string{"session_id", "user_id", "task_count", "tokens_used", "created_at"},
					"properties": map[string]interface{}{
						"session_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "Unique session identifier",
						},
						"user_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "User who owns this session",
						},
						"task_count": map[string]interface{}{
							"type":        "integer",
							"description": "Number of tasks in this session",
						},
						"tokens_used": map[string]interface{}{
							"type":        "integer",
							"description": "Total tokens consumed in this session",
						},
						"token_budget": map[string]interface{}{
							"type":        "integer",
							"description": "Token budget for the session",
						},
						"created_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session creation timestamp",
						},
						"updated_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session last update timestamp",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session expiration timestamp",
						},
					},
				},
				"SessionResponse": map[string]interface{}{
					"type":     "object",
					"required": []string{"session_id", "user_id", "task_count", "tokens_used", "created_at"},
					"properties": map[string]interface{}{
						"session_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "Unique session identifier",
						},
						"user_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "User who owns this session",
						},
						"context": map[string]interface{}{
							"type":        "object",
							"description": "Session context metadata",
						},
						"token_budget": map[string]interface{}{
							"type":        "integer",
							"description": "Token budget for the session",
						},
						"tokens_used": map[string]interface{}{
							"type":        "integer",
							"description": "Total tokens consumed in this session",
						},
						"task_count": map[string]interface{}{
							"type":        "integer",
							"description": "Number of tasks in this session",
						},
						"created_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session creation timestamp",
						},
						"updated_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session last update timestamp",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Session expiration timestamp",
						},
					},
				},
				"TaskHistory": map[string]interface{}{
					"type":     "object",
					"required": []string{"task_id", "workflow_id", "query", "status", "started_at"},
					"properties": map[string]interface{}{
						"task_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "Task UUID",
						},
						"workflow_id": map[string]interface{}{
							"type":        "string",
							"description": "Temporal workflow ID",
						},
						"query": map[string]interface{}{
							"type":        "string",
							"description": "Task query/command",
						},
						"status": map[string]interface{}{
							"type":        "string",
							"description": "Task execution status",
							"enum":        []string{"RUNNING", "COMPLETED", "FAILED", "CANCELLED", "TIMEOUT"},
						},
						"mode": map[string]interface{}{
							"type":        "string",
							"description": "Execution mode",
						},
						"result": map[string]interface{}{
							"type":        "string",
							"description": "Task result/output",
						},
						"error_message": map[string]interface{}{
							"type":        "string",
							"description": "Error message if task failed",
						},
						"total_tokens": map[string]interface{}{
							"type":        "integer",
							"description": "Total tokens used",
						},
						"total_cost_usd": map[string]interface{}{
							"type":        "number",
							"format":      "double",
							"description": "Total cost in USD",
						},
						"duration_ms": map[string]interface{}{
							"type":        "integer",
							"description": "Task duration in milliseconds",
						},
						"agents_used": map[string]interface{}{
							"type":        "integer",
							"description": "Number of agents used",
						},
						"tools_invoked": map[string]interface{}{
							"type":        "integer",
							"description": "Number of tools invoked",
						},
						"started_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Task start timestamp",
						},
						"completed_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "Task completion timestamp",
						},
						"metadata": map[string]interface{}{
							"type":        "object",
							"description": "Additional task metadata",
						},
					},
				},
				"SessionHistoryResponse": map[string]interface{}{
					"type":     "object",
					"required": []string{"session_id", "tasks", "total"},
					"properties": map[string]interface{}{
						"session_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "Session UUID",
						},
						"tasks": map[string]interface{}{
							"type":        "array",
							"description": "List of tasks in chronological order",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/TaskHistory",
							},
						},
						"total": map[string]interface{}{
							"type":        "integer",
							"description": "Total number of tasks in session",
						},
					},
				},
				"SessionEventsResponse": map[string]interface{}{
					"type":     "object",
					"required": []string{"session_id", "events", "count"},
					"properties": map[string]interface{}{
						"session_id": map[string]interface{}{
							"type":        "string",
							"format":      "uuid",
							"description": "Session UUID",
						},
						"events": map[string]interface{}{
							"type":        "array",
							"description": "List of events in chronological order (excludes LLM_PARTIAL)",
							"items": map[string]interface{}{
								"$ref": "#/components/schemas/TaskEvent",
							},
						},
						"count": map[string]interface{}{
							"type":        "integer",
							"description": "Number of events returned",
						},
					},
				},
			},
		},
	}
}
