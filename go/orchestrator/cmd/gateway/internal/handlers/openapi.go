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
						"status": map[string]interface{}{
							"type": "string",
						},
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
						"session_id": map[string]interface{}{
							"type":        "string",
							"description": "Session ID for context continuity",
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
						"status": map[string]interface{}{
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
				"TaskStatusResponse": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"task_id": map[string]interface{}{
							"type": "string",
						},
						"status": map[string]interface{}{
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
						"session_id": map[string]interface{}{
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
			},
		},
	}
}
