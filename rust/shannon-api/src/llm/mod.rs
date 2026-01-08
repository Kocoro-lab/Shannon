//! LLM driver traits and implementations.
//!
//! This module provides protocol-agnostic abstractions for interacting with
//! Large Language Models, supporting OpenAI, Anthropic, Google, and other providers.
//!
//! # Overview
//!
//! The [`LlmDriver`] trait defines the core streaming interface that all
//! LLM implementations must support. The [`Orchestrator`] builds on top
//! of drivers to provide tool loop execution.
//!
//! # Drivers
//!
//! - [`providers::OpenAiDriver`]: OpenAI and compatible APIs
//! - [`providers::AnthropicDriver`]: Anthropic Claude API
//! - [`providers::GoogleDriver`]: Google Gemini API
//! - [`providers::GroqDriver`]: Groq API

pub mod orchestrator;
pub mod providers;

use std::pin::Pin;

use crate::events::NormalizedEvent;
use async_trait::async_trait;
use futures::Stream;
use serde::{Deserialize, Serialize};

/// LLM connection and model settings.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LlmSettings {
    /// Base URL for the LLM API.
    pub base_url: String,
    /// API key for authentication.
    pub api_key: Option<String>,
    /// Model identifier.
    pub model: String,
    /// Provider type.
    pub provider: Provider,
    /// Maximum tokens to generate.
    #[serde(default = "default_max_tokens")]
    pub max_tokens: u32,
    /// Temperature for sampling.
    #[serde(default = "default_temperature")]
    pub temperature: f32,
    /// Whether to enable parallel tool calls.
    #[serde(default)]
    pub parallel_tool_calls: Option<bool>,
}

fn default_max_tokens() -> u32 {
    4096
}

fn default_temperature() -> f32 {
    0.7
}

impl Default for LlmSettings {
    fn default() -> Self {
        Self {
            base_url: "https://api.openai.com".to_string(),
            api_key: None,
            model: "gpt-4o".to_string(),
            provider: Provider::OpenAi,
            max_tokens: default_max_tokens(),
            temperature: default_temperature(),
            parallel_tool_calls: None,
        }
    }
}

/// Supported LLM providers.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "lowercase")]
pub enum Provider {
    /// OpenAI and compatible APIs.
    #[default]
    OpenAi,
    /// Anthropic Claude.
    Anthropic,
    /// Google Gemini.
    Google,
    /// Groq.
    Groq,
    /// xAI Grok.
    Xai,
    /// Custom/unknown provider.
    Custom,
}

impl Provider {
    /// Get the default base URL for this provider.
    pub fn default_base_url(&self) -> &'static str {
        match self {
            Self::OpenAi => "https://api.openai.com",
            Self::Anthropic => "https://api.anthropic.com",
            Self::Google => "https://generativelanguage.googleapis.com",
            Self::Groq => "https://api.groq.com",
            Self::Xai => "https://api.x.ai",
            Self::Custom => "",
        }
    }

    /// Detect provider from base URL.
    pub fn from_base_url(url: &str) -> Self {
        if url.contains("openai.com") {
            Self::OpenAi
        } else if url.contains("anthropic.com") {
            Self::Anthropic
        } else if url.contains("googleapis.com") || url.contains("google.com") {
            Self::Google
        } else if url.contains("groq.com") {
            Self::Groq
        } else if url.contains("x.ai") {
            Self::Xai
        } else {
            Self::Custom
        }
    }
}

/// A message in a conversation.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Message {
    /// Role of the message author.
    pub role: MessageRole,
    /// Content of the message.
    pub content: MessageContent,
    /// Optional tool call ID (for tool responses).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_call_id: Option<String>,
    /// Tool calls made by the assistant.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tool_calls: Option<Vec<ToolCall>>,
}

impl Message {
    /// Create a system message.
    pub fn system(content: impl Into<String>) -> Self {
        Self {
            role: MessageRole::System,
            content: MessageContent::Text(content.into()),
            tool_call_id: None,
            tool_calls: None,
        }
    }

    /// Create a user message.
    pub fn user(content: impl Into<String>) -> Self {
        Self {
            role: MessageRole::User,
            content: MessageContent::Text(content.into()),
            tool_call_id: None,
            tool_calls: None,
        }
    }

    /// Create an assistant message.
    pub fn assistant(content: impl Into<String>) -> Self {
        Self {
            role: MessageRole::Assistant,
            content: MessageContent::Text(content.into()),
            tool_call_id: None,
            tool_calls: None,
        }
    }

    /// Create a tool response message.
    pub fn tool_result(tool_call_id: impl Into<String>, content: impl Into<String>) -> Self {
        Self {
            role: MessageRole::Tool,
            content: MessageContent::Text(content.into()),
            tool_call_id: Some(tool_call_id.into()),
            tool_calls: None,
        }
    }
}

/// Message content - either simple text or multimodal parts.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(untagged)]
pub enum MessageContent {
    /// Simple text content.
    Text(String),
    /// Multimodal content with text and image parts.
    Parts(Vec<ContentPart>),
}

impl MessageContent {
    /// Get the text content.
    pub fn as_text(&self) -> Option<&str> {
        match self {
            Self::Text(s) => Some(s),
            Self::Parts(parts) => parts.iter().find_map(|p| {
                if let ContentPart::Text { text } = p {
                    Some(text.as_str())
                } else {
                    None
                }
            }),
        }
    }
}

impl Default for MessageContent {
    fn default() -> Self {
        Self::Text(String::new())
    }
}

/// A content part for multimodal messages.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum ContentPart {
    /// Text content.
    Text {
        /// The text content.
        text: String,
    },
    /// Image content (URL or base64 data URL).
    ImageUrl {
        /// Image URL configuration.
        image_url: ImageUrl,
    },
}

/// Image URL configuration.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ImageUrl {
    /// Image URL (can be HTTP URL or base64 data URL).
    pub url: String,
    /// Detail level: "auto", "low", or "high".
    #[serde(skip_serializing_if = "Option::is_none")]
    pub detail: Option<String>,
}

/// Role of a message author.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum MessageRole {
    /// System prompt.
    System,
    /// User message.
    User,
    /// Assistant response.
    Assistant,
    /// Tool response.
    Tool,
}

/// A tool call made by the assistant.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCall {
    /// Unique identifier for this tool call.
    pub id: String,
    /// Type of tool (always "function" for now).
    #[serde(rename = "type")]
    pub call_type: String,
    /// Function details.
    pub function: ToolCallFunction,
}

/// Function details in a tool call.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCallFunction {
    /// Function name.
    pub name: String,
    /// Arguments as JSON string.
    pub arguments: String,
}

/// Request to an LLM driver.
#[derive(Debug)]
pub struct LlmRequest {
    /// Conversation messages.
    pub messages: Vec<Message>,
    /// Available tools in OpenAI function schema format.
    pub tools: Vec<serde_json::Value>,
    /// Model to use (overrides settings).
    pub model: Option<String>,
    /// Temperature (overrides settings).
    pub temperature: Option<f32>,
    /// Max tokens (overrides settings).
    pub max_tokens: Option<u32>,
}

impl LlmRequest {
    /// Create a new request with messages.
    pub fn new(messages: Vec<Message>) -> Self {
        Self {
            messages,
            tools: Vec::new(),
            model: None,
            temperature: None,
            max_tokens: None,
        }
    }

    /// Add tools to the request.
    pub fn with_tools(mut self, tools: Vec<serde_json::Value>) -> Self {
        self.tools = tools;
        self
    }
}

/// Trait for LLM streaming drivers.
#[async_trait]
pub trait LlmDriver: Send + Sync {
    /// Stream a response from the LLM.
    async fn stream(
        &self,
        req: LlmRequest,
    ) -> anyhow::Result<Pin<Box<dyn Stream<Item = anyhow::Result<NormalizedEvent>> + Send>>>;

    /// Get the provider type.
    fn provider(&self) -> Provider;

    /// Get the current settings.
    fn settings(&self) -> &LlmSettings;
}
