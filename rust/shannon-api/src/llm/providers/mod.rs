//! LLM provider implementations.

mod openai;
mod anthropic;

pub use openai::OpenAiDriver;
pub use anthropic::AnthropicDriver;

use super::{LlmDriver, LlmSettings, Provider};
use std::sync::Arc;

/// Create a driver for the given settings.
pub fn create_driver(settings: LlmSettings) -> Arc<dyn LlmDriver> {
    match settings.provider {
        Provider::OpenAi | Provider::Groq | Provider::Xai | Provider::Custom => {
            Arc::new(OpenAiDriver::new(settings))
        }
        Provider::Anthropic => Arc::new(AnthropicDriver::new(settings)),
        Provider::Google => {
            // Google uses OpenAI-compatible API for Gemini
            Arc::new(OpenAiDriver::new(settings))
        }
    }
}
