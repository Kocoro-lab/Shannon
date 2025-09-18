#[cfg(feature = "wasi")]
use crate::wasi_sandbox::WasiSandbox;
use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use tracing::{debug, info, warn};

use base64::Engine;
use tokio::fs;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolCall {
    pub tool_name: String,
    pub parameters: HashMap<String, serde_json::Value>,
    pub call_id: Option<String>,
}

#[cfg(test)]
mod tests {
    use super::*;

    // Same minimal wasm as in wasi_sandbox tests
    const MINIMAL_WASM: &[u8] = &[
        0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00, 0x01, 0x04, 0x01, 0x60, 0x00, 0x00, 0x03,
        0x02, 0x01, 0x00, 0x07, 0x0a, 0x01, 0x06, 0x5f, 0x73, 0x74, 0x61, 0x72, 0x74, 0x00, 0x00,
        0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
    ];

    #[tokio::test]
    #[cfg(feature = "wasi")]
    async fn test_code_executor_with_base64_payload() {
        let wasi = WasiSandbox::new().expect("sandbox");
        let exec = ToolExecutor::new_with_wasi(Some(wasi), None);

        let b64 = base64::engine::general_purpose::STANDARD.encode(MINIMAL_WASM);
        let mut params = HashMap::new();
        params.insert("wasm_base64".to_string(), serde_json::Value::String(b64));

        let call = ToolCall {
            tool_name: "code_executor".to_string(),
            parameters: params,
            call_id: None,
        };
        let res = exec.execute_tool(&call).await.expect("tool result");
        assert!(res.success, "expected success: {:?}", res.error);
        assert_eq!(res.output, serde_json::Value::String(String::new()));
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ToolResult {
    pub tool: String,
    pub success: bool,
    pub output: serde_json::Value,
    pub error: Option<String>,
}

pub struct ToolExecutor {
    llm_service_url: String,
    #[cfg(feature = "wasi")]
    wasi: Option<WasiSandbox>,
}

impl ToolExecutor {
    pub fn new(llm_service_url: Option<String>) -> Self {
        Self {
            llm_service_url: llm_service_url
                .or_else(|| std::env::var("LLM_SERVICE_URL").ok())
                .unwrap_or_else(|| "http://llm-service:8000".to_string()),
            #[cfg(feature = "wasi")]
            wasi: None,
        }
    }

    #[cfg(feature = "wasi")]
    pub fn new_with_wasi(wasi: Option<WasiSandbox>, llm_service_url: Option<String>) -> Self {
        Self {
            llm_service_url: llm_service_url
                .or_else(|| std::env::var("LLM_SERVICE_URL").ok())
                .unwrap_or_else(|| "http://llm-service:8000".to_string()),
            wasi,
        }
    }

    #[cfg(not(feature = "wasi"))]
    pub fn new_with_wasi(_wasi: Option<()>, llm_service_url: Option<String>) -> Self {
        Self {
            llm_service_url: llm_service_url
                .or_else(|| std::env::var("LLM_SERVICE_URL").ok())
                .unwrap_or_else(|| "http://llm-service:8000".to_string()),
        }
    }

    #[cfg(feature = "wasi")]
    pub fn set_wasi(&mut self, wasi: Option<WasiSandbox>) {
        self.wasi = wasi;
    }

    #[cfg(not(feature = "wasi"))]
    pub fn set_wasi(&mut self, _wasi: Option<()>) {
        // No-op when WASI is disabled
    }

    /// Select tools remotely (stub implementation)
    pub async fn select_tools_remote(
        &self,
        _task: &str,
        _exclude_dangerous: bool,
    ) -> Result<Vec<String>> {
        // Stub implementation - return basic tools for math calculations
        Ok(vec!["calculator".to_string()])
    }

    /// Execute a tool via the LLM service
    pub async fn execute_tool(&self, tool_call: &ToolCall) -> Result<ToolResult> {
        info!(
            "Executing tool: {} with parameters: {:?}",
            tool_call.tool_name, tool_call.parameters
        );

        // Route calculator to local execution
        if tool_call.tool_name == "calculator" {
            if let Some(expression) = tool_call
                .parameters
                .get("expression")
                .and_then(|v| v.as_str())
            {
                info!(
                    "Executing calculator locally with expression: {}",
                    expression
                );

                // Use meval for mathematical expression evaluation
                match meval::eval_str(expression) {
                    Ok(result) => {
                        info!("Calculator result: {}", result);
                        return Ok(ToolResult {
                            tool: tool_call.tool_name.clone(),
                            success: true,
                            output: serde_json::json!({
                                "result": result,
                                "expression": expression
                            }),
                            error: None,
                        });
                    }
                    Err(e) => {
                        warn!("Calculator evaluation error: {}", e);
                        return Ok(ToolResult {
                            tool: tool_call.tool_name.clone(),
                            success: false,
                            output: serde_json::Value::Null,
                            error: Some(format!("Math evaluation error: {}", e)),
                        });
                    }
                }
            } else {
                return Ok(ToolResult {
                    tool: tool_call.tool_name.clone(),
                    success: false,
                    output: serde_json::Value::Null,
                    error: Some("Missing 'expression' parameter for calculator".to_string()),
                });
            }
        }

        // Route code execution to WASI sandbox when requested
        #[cfg(feature = "wasi")]
        if tool_call.tool_name == "code_executor" {
            if let Some(wasi) = &self.wasi {
                // Expect a wasm module path and optional stdin
                let stdin = tool_call
                    .parameters
                    .get("stdin")
                    .and_then(|v| v.as_str())
                    .unwrap_or("");

                // Extract argv parameters if provided (needed for Python WASM)
                let argv = tool_call
                    .parameters
                    .get("argv")
                    .and_then(|v| v.as_array())
                    .map(|arr| {
                        arr.iter()
                            .filter_map(|v| v.as_str().map(String::from))
                            .collect::<Vec<String>>()
                    });

                debug!("code_executor: stdin length={}, argv={:?}", stdin.len(), argv);

                // Prefer base64 payload if provided
                let wasm_bytes_res: Result<Vec<u8>> = if let Some(b64) = tool_call
                    .parameters
                    .get("wasm_base64")
                    .and_then(|v| v.as_str())
                {
                    base64::engine::general_purpose::STANDARD
                        .decode(b64.trim())
                        .context("Failed to decode wasm_base64 payload")
                } else if let Some(path_val) = tool_call
                    .parameters
                    .get("wasm_path")
                    .and_then(|v| v.as_str())
                {
                    fs::read(path_val)
                        .await
                        .with_context(|| format!("Failed to read wasm module at {}", path_val))
                } else {
                    Err(anyhow::anyhow!(
                        "missing 'wasm_base64' or 'wasm_path' parameter"
                    ))
                };

                match wasm_bytes_res {
                    Ok(bytes) => match wasi.execute_wasm_with_args(&bytes, stdin, argv).await {
                        Ok(output) => {
                            return Ok(ToolResult {
                                tool: tool_call.tool_name.clone(),
                                success: true,
                                output: serde_json::Value::String(output),
                                error: None,
                            });
                        }
                        Err(e) => {
                            let msg = format!("WASI execution error: {}", e);
                            warn!("{}", msg);
                            return Ok(ToolResult {
                                tool: tool_call.tool_name.clone(),
                                success: false,
                                output: serde_json::Value::Null,
                                error: Some(msg),
                            });
                        }
                    },
                    Err(e) => {
                        warn!("code_executor parameter error: {}", e);
                        return Ok(ToolResult {
                            tool: tool_call.tool_name.clone(),
                            success: false,
                            output: serde_json::Value::Null,
                            error: Some(e.to_string()),
                        });
                    }
                }
            } else {
                warn!("WASI sandbox not configured; falling back to HTTP tool execution");
            }
        }

        let client = reqwest::Client::new();
        let url = format!("{}/tools/execute", self.llm_service_url);

        let request_body = serde_json::json!({
            "tool_name": tool_call.tool_name,
            "parameters": tool_call.parameters,
        });

        let response = client.post(&url).json(&request_body).send().await?;

        if !response.status().is_success() {
            let error_text = response.text().await?;
            warn!("Tool execution failed: {}", error_text);
            return Ok(ToolResult {
                tool: tool_call.tool_name.clone(),
                success: false,
                output: serde_json::Value::Null,
                error: Some(error_text),
            });
        }

        let result: serde_json::Value = response.json().await?;

        Ok(ToolResult {
            tool: tool_call.tool_name.clone(),
            success: result["success"].as_bool().unwrap_or(false),
            output: result["output"].clone(),
            error: result["error"].as_str().map(String::from),
        })
    }

    /// Get available tools from the LLM service
    pub async fn get_available_tools(&self, exclude_dangerous: bool) -> Result<Vec<String>> {
        debug!("Fetching available tools");

        let client = reqwest::Client::new();
        let url = format!(
            "{}/tools/list?exclude_dangerous={}",
            self.llm_service_url, exclude_dangerous
        );

        let response = client.get(&url).send().await?;

        if !response.status().is_success() {
            warn!("Failed to fetch available tools");
            return Ok(vec![]);
        }

        let tools: Vec<String> = response.json().await?;
        debug!("Available tools: {:?}", tools);

        Ok(tools)
    }

}
