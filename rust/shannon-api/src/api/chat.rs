//! Chat completions API endpoints.
//!
//! This module provides OpenAI-compatible chat completion endpoints.

use axum::{
    extract::State,
    response::sse::{Event, Sse},
    routing::post,
    Json, Router,
};
use futures::StreamExt;
use serde::{Deserialize, Serialize};
use std::convert::Infallible;

use crate::llm::Message;
use crate::AppState;

/// Create the chat router.
pub fn router() -> Router<AppState> {
    Router::new()
        .route("/v1/chat/completions", post(chat_completions))
        .route("/agent/chat", post(agent_chat))
}

/// OpenAI-compatible chat completions request.
#[derive(Debug, Deserialize)]
pub struct ChatCompletionsRequest {
    /// Model to use.
    pub model: Option<String>,
    /// Messages in the conversation.
    pub messages: Vec<ChatMessage>,
    /// Whether to stream the response.
    #[serde(default)]
    pub stream: bool,
    /// Temperature for sampling.
    pub temperature: Option<f32>,
    /// Maximum tokens to generate.
    pub max_tokens: Option<u32>,
    /// Tools available for the model.
    #[serde(default)]
    pub tools: Vec<serde_json::Value>,
}

/// Chat message in the request.
#[derive(Debug, Deserialize)]
pub struct ChatMessage {
    /// Role of the message author.
    pub role: String,
    /// Content of the message.
    pub content: Option<String>,
    /// Tool call ID (for tool responses).
    pub tool_call_id: Option<String>,
    /// Tool calls made by the assistant.
    pub tool_calls: Option<Vec<serde_json::Value>>,
}

impl From<ChatMessage> for Message {
    fn from(msg: ChatMessage) -> Self {
        let role = match msg.role.as_str() {
            "system" => crate::llm::MessageRole::System,
            "user" => crate::llm::MessageRole::User,
            "assistant" => crate::llm::MessageRole::Assistant,
            "tool" => crate::llm::MessageRole::Tool,
            _ => crate::llm::MessageRole::User,
        };

        Message {
            role,
            content: crate::llm::MessageContent::Text(msg.content.unwrap_or_default()),
            tool_call_id: msg.tool_call_id,
            tool_calls: None, // TODO: Convert tool calls
        }
    }
}

/// Chat completions endpoint (OpenAI-compatible).
async fn chat_completions(
    State(state): State<AppState>,
    Json(req): Json<ChatCompletionsRequest>,
) -> Result<Sse<impl futures::Stream<Item = Result<Event, Infallible>>>, axum::http::StatusCode> {
    let messages: Vec<Message> = req.messages.into_iter().map(Into::into).collect();

    let orchestrator = state.orchestrator.clone();
    let stream = orchestrator
        .chat_with_tools(messages, req.tools)
        .await
        .map_err(|e| {
            tracing::error!("Chat error: {}", e);
            axum::http::StatusCode::INTERNAL_SERVER_ERROR
        })?;

    let sse_stream = stream.map(|event| {
        let data = serde_json::to_string(&event).unwrap_or_default();
        Ok::<_, Infallible>(Event::default().event(event.event_type()).data(data))
    });

    Ok(Sse::new(sse_stream))
}

/// Agent chat request.
#[derive(Debug, Deserialize)]
pub struct AgentChatRequest {
    /// The query/prompt.
    pub query: String,
    /// Session ID (optional, for conversation continuity).
    pub session_id: Option<String>,
    /// User ID (optional).
    pub user_id: Option<String>,
    /// Additional context.
    #[serde(default)]
    pub context: serde_json::Value,
}

/// Agent chat response.
#[derive(Debug, Serialize)]
pub struct AgentChatResponse {
    /// Run ID for tracking.
    pub run_id: String,
    /// Session ID.
    pub session_id: Option<String>,
    /// Status.
    pub status: String,
}

/// Agent chat endpoint.
async fn agent_chat(
    State(state): State<AppState>,
    Json(req): Json<AgentChatRequest>,
) -> Result<Sse<impl futures::Stream<Item = Result<Event, Infallible>>>, axum::http::StatusCode> {
    let (run_id, receiver) = state
        .run_manager
        .start_run(&req.query, req.session_id.clone(), req.user_id)
        .await
        .map_err(|e| {
            tracing::error!("Failed to start run: {}", e);
            axum::http::StatusCode::INTERNAL_SERVER_ERROR
        })?;

    // Convert broadcast receiver to stream
    let stream = tokio_stream::wrappers::BroadcastStream::new(receiver);

    let sse_stream = stream.filter_map(|result| async move {
        match result {
            Ok(event) => {
                let data = serde_json::to_string(&event).unwrap_or_default();
                Some(Ok::<_, Infallible>(
                    Event::default().event(event.event_type()).data(data),
                ))
            }
            Err(_) => None, // Skip lagged events
        }
    });

    // Prepend a run_started event
    let run_started = futures::stream::once(async move {
        let data = serde_json::json!({
            "run_id": run_id,
            "status": "running"
        });
        Ok::<_, Infallible>(
            Event::default()
                .event("run_started")
                .data(serde_json::to_string(&data).unwrap_or_default()),
        )
    });

    let combined = run_started.chain(sse_stream);

    Ok(Sse::new(combined))
}
