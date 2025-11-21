use tokio::sync::mpsc;
use tokio_stream::StreamExt;
use tonic::{Request, Response, Status};
use tracing::{debug, error, info};

// FSM removed; Rust acts as an enforcement gateway
use crate::enforcement::RequestEnforcer;
use crate::llm_client::LLMClient;
use crate::memory::MemoryPool;

#[cfg(feature = "wasi")]
use crate::wasi_sandbox::WasiSandbox;

// Include the generated proto code
#[allow(clippy::enum_variant_names)]
pub mod proto {
    pub mod agent {
        tonic::include_proto!("shannon.agent");
    }
    #[allow(clippy::enum_variant_names)]
    pub mod common {
        tonic::include_proto!("shannon.common");
    }

    // Export file descriptor for reflection
    pub const FILE_DESCRIPTOR_SET: &[u8] =
        tonic::include_file_descriptor_set!("shannon_descriptor");
}

use proto::agent::agent_service_server::{AgentService, AgentServiceServer};
use proto::agent::*;

pub struct AgentServiceImpl {
    memory_pool: MemoryPool,
    #[cfg(feature = "wasi")]
    sandbox: WasiSandbox,
    start_time: std::time::Instant,
    llm: std::sync::Arc<LLMClient>,
    enforcer: std::sync::Arc<RequestEnforcer>,
}

impl Default for AgentServiceImpl {
    fn default() -> Self {
        // Default implementation uses unwrap - should only be used in tests
        Self::new().expect("Failed to create AgentServiceImpl in Default trait")
    }
}

// Thread safety: AgentServiceImpl is Send + Sync because:
// - memory_pool: MemoryPool uses Arc<RwLock<>> internally for thread safety
// - sandbox: WasiSandbox uses Arc<Engine> for thread safety and contains only basic types
//            that are Send/Sync, with methods thread-safe for concurrent execution
// - start_time: std::time::Instant is Send + Sync
//
// Both MemoryPool and WasiSandbox automatically implement Send + Sync since all their
// fields are Send + Sync, so AgentServiceImpl also automatically implements them.

impl AgentServiceImpl {
    pub fn new() -> anyhow::Result<Self> {
        // Get sweep interval from environment or use default of 10 seconds
        let sweep_interval_ms = std::env::var("MEMORY_SWEEP_INTERVAL_MS")
            .unwrap_or_else(|_| "10000".to_string())
            .parse()
            .unwrap_or(10000);

        Ok(Self {
            memory_pool: MemoryPool::new(512).start_sweeper(sweep_interval_ms), // 512MB memory pool with sweeper
            #[cfg(feature = "wasi")]
            sandbox: WasiSandbox::new()?,
            start_time: std::time::Instant::now(),
            llm: std::sync::Arc::new(LLMClient::new(None)?),
            enforcer: std::sync::Arc::new(RequestEnforcer::from_global()?),
        })
    }

    pub fn into_service(self) -> AgentServiceServer<Self> {
        AgentServiceServer::new(self)
    }

    /// LLM-native direct tool execution - bypass FSM entirely
    async fn execute_direct_tool(
        &self,
        tool_params: &prost_types::Value,
        req: &ExecuteTaskRequest,
    ) -> Result<Response<ExecuteTaskResponse>, Status> {
        use crate::tools::{ToolCall, ToolExecutor};
        use prost_types::{Struct, Value};
        use std::collections::HashMap;

        info!("Executing LLM-suggested tool directly");

        // Extract tool parameters from prost Value (strict: must include tool + args)
        let (tool_name, parameters) = match &tool_params.kind {
            Some(prost_types::value::Kind::StructValue(s)) => {
                let tool_name = s
                    .fields
                    .get("tool")
                    .and_then(|v| v.kind.as_ref())
                    .and_then(|k| match k {
                        prost_types::value::Kind::StringValue(s) => Some(s.clone()),
                        _ => None,
                    })
                    .ok_or_else(|| {
                        Status::invalid_argument(
                            "tool_parameters missing required 'tool' string field",
                        )
                    })?;

                // Enforce allowed tools if provided in request
                if !req.available_tools.is_empty() && !req.available_tools.contains(&tool_name) {
                    return Err(Status::permission_denied(format!(
                        "Tool '{}' is not allowed for this request",
                        tool_name
                    )));
                }

                // Helper function for recursive prost Value to JSON conversion
                fn prost_to_json_recursive(prost_val: &prost_types::Value) -> serde_json::Value {
                    match prost_val.kind.as_ref() {
                        Some(prost_types::value::Kind::StringValue(s)) => {
                            serde_json::Value::String(s.clone())
                        }
                        Some(prost_types::value::Kind::NumberValue(n)) => {
                            serde_json::Value::Number(
                                serde_json::Number::from_f64(*n)
                                    .unwrap_or_else(|| serde_json::Number::from(0)),
                            )
                        }
                        Some(prost_types::value::Kind::BoolValue(b)) => serde_json::Value::Bool(*b),
                        Some(prost_types::value::Kind::NullValue(_)) => serde_json::Value::Null,
                        Some(prost_types::value::Kind::ListValue(list)) => {
                            serde_json::Value::Array(
                                list.values
                                    .iter()
                                    .map(|v| prost_to_json_recursive(v))
                                    .collect(),
                            )
                        }
                        Some(prost_types::value::Kind::StructValue(st)) => {
                            // Recursive convert nested struct
                            let mut map = serde_json::Map::new();
                            for (k, v) in &st.fields {
                                map.insert(k.clone(), prost_to_json_recursive(v));
                            }
                            serde_json::Value::Object(map)
                        }
                        None => serde_json::Value::Null,
                    }
                }

                // Convert fields (except 'tool') to JSON params
                let mut params = HashMap::new();
                for (key, value) in &s.fields {
                    if key == "tool" {
                        continue;
                    }
                    let json_val = prost_to_json_recursive(value);
                    params.insert(key.clone(), json_val);
                }

                // Strict: do NOT infer parameters from req.query; require LLM-provided args
                if tool_name == "calculator" && !params.contains_key("expression") {
                    return Err(Status::invalid_argument(
                        "calculator requires 'expression' parameter",
                    ));
                }

                (tool_name, params)
            }
            _ => {
                return Err(Status::invalid_argument(
                    "tool_parameters must be an object",
                ));
            }
        };

        // Create and execute tool call
        let tool_call = ToolCall {
            tool_name: tool_name.clone(),
            parameters,
            call_id: Some(format!(
                "llm-direct-{}",
                chrono::Utc::now().timestamp_millis()
            )),
        };

        #[cfg(feature = "wasi")]
        let tool_executor = ToolExecutor::new_with_wasi(Some(self.sandbox.clone()), None);
        #[cfg(not(feature = "wasi"))]
        let tool_executor = ToolExecutor::new_with_wasi(None, None);

        // Measure execution time
        let start_time = std::time::Instant::now();
        match tool_executor
            .execute_tool(&tool_call, req.context.as_ref())
            .await
        {
            Ok(tool_result) => {
                let execution_time_ms = start_time.elapsed().as_millis() as i64;
                // Prefer a simple, user-facing response: if the tool output
                // contains a top-level "result" or is a primitive, surface that;
                // otherwise fall back to compact JSON string.
                let result_text = extract_simple_text_from_json(&tool_result.output);

                // Convert parameters back to Struct for response
                fn to_prost_value(v: &serde_json::Value) -> Value {
                    match v {
                        serde_json::Value::Null => Value {
                            kind: Some(prost_types::value::Kind::NullValue(0)),
                        },
                        serde_json::Value::Bool(b) => Value {
                            kind: Some(prost_types::value::Kind::BoolValue(*b)),
                        },
                        serde_json::Value::Number(n) => Value {
                            kind: Some(prost_types::value::Kind::NumberValue(
                                n.as_f64().unwrap_or(0.0),
                            )),
                        },
                        serde_json::Value::String(s) => Value {
                            kind: Some(prost_types::value::Kind::StringValue(s.clone())),
                        },
                        serde_json::Value::Array(arr) => Value {
                            kind: Some(prost_types::value::Kind::ListValue(
                                prost_types::ListValue {
                                    values: arr.iter().map(to_prost_value).collect(),
                                },
                            )),
                        },
                        serde_json::Value::Object(map) => Value {
                            kind: Some(prost_types::value::Kind::StructValue(Struct {
                                fields: map
                                    .iter()
                                    .map(|(k, v)| (k.clone(), to_prost_value(v)))
                                    .collect(),
                            })),
                        },
                    }
                }

                let params_struct = Struct {
                    fields: tool_call
                        .parameters
                        .iter()
                        .map(|(k, v)| (k.clone(), to_prost_value(v)))
                        .collect(),
                };

                // Build tool_call and tool_result for response
                let resp_tool_call = proto::common::ToolCall {
                    name: tool_call.tool_name.clone(),
                    parameters: Some(params_struct),
                    tool_id: tool_call.tool_name.clone(),
                };

                let resp_tool_result = proto::common::ToolResult {
                    tool_id: tool_call.tool_name.clone(),
                    output: Some(to_prost_value(&tool_result.output)),
                    status: if tool_result.success {
                        proto::common::StatusCode::Ok.into()
                    } else {
                        proto::common::StatusCode::Error.into()
                    },
                    error_message: tool_result.error.clone().unwrap_or_default(),
                    execution_time_ms,
                };

                info!("LLM-native tool execution completed: {}", tool_name);

                let response = ExecuteTaskResponse {
                    task_id: req
                        .metadata
                        .as_ref()
                        .map(|m| m.task_id.clone())
                        .unwrap_or_default(),
                    status: if tool_result.success {
                        proto::common::StatusCode::Ok.into()
                    } else {
                        proto::common::StatusCode::Error.into()
                    },
                    result: result_text,
                    tool_calls: vec![resp_tool_call],
                    tool_results: vec![resp_tool_result],
                    metrics: Some(proto::common::ExecutionMetrics {
                        latency_ms: 100,
                        token_usage: None, // No LLM tokens used for direct tool execution
                        cache_hit: false,
                        cache_score: 0.0,
                        agents_used: 0, // Direct execution, no agents
                        mode: req.mode,
                    }),
                    error_message: tool_result.error.unwrap_or_default(),
                    final_state: if tool_result.success {
                        proto::agent::AgentState::Completed.into()
                    } else {
                        proto::agent::AgentState::Failed.into()
                    },
                };

                tracing::info!(
                    "ExecuteTaskResponse (direct tool): token_usage=None, tool={}, ms={}",
                    tool_name,
                    execution_time_ms
                );
                Ok(Response::new(response))
            }
            Err(e) => {
                let execution_time_ms = start_time.elapsed().as_millis() as i64;
                error!(
                    "LLM-native tool execution failed in {}ms: {}",
                    execution_time_ms, e
                );
                Err(Status::internal(format!("Tool execution failed: {}", e)))
            }
        }
    }

    /// Execute a sequence of tool calls provided by Python in context.tool_calls
    async fn execute_tool_calls(
        &self,
        list: &prost_types::ListValue,
        req: &ExecuteTaskRequest,
    ) -> Result<Response<ExecuteTaskResponse>, Status> {
        use crate::tools::{ToolCall, ToolExecutor};
        use prost_types::Value;
        use std::collections::HashMap;

        fn to_json(v: &Value) -> serde_json::Value {
            crate::grpc_server::prost_value_to_json(v)
        }

        #[cfg(feature = "wasi")]
        let tool_executor = ToolExecutor::new_with_wasi(Some(self.sandbox.clone()), None);
        #[cfg(not(feature = "wasi"))]
        let tool_executor = ToolExecutor::new_with_wasi(None, None);
        let mut tool_calls_vec = Vec::new();
        let mut tool_results_vec = Vec::new();
        let mut overall_status = proto::common::StatusCode::Ok.into();
        let mut last_output = serde_json::Value::Null;

        let mut cumulative_ms: i64 = 0;
        let mut failure_msgs: Vec<String> = Vec::new();
        let total = list.values.len();

        // Optional secondary allowlist from context (defense-in-depth)
        let mut ctx_allowed: Option<std::collections::HashSet<String>> = None;
        if let Some(ctx) = &req.context {
            if let Some(val) = ctx.fields.get("allowed_tools") {
                if let Some(prost_types::value::Kind::ListValue(lv)) = &val.kind {
                    let mut s = std::collections::HashSet::new();
                    for v in &lv.values {
                        if let Some(prost_types::value::Kind::StringValue(name)) = &v.kind {
                            s.insert(name.clone());
                        }
                    }
                    if !s.is_empty() {
                        ctx_allowed = Some(s);
                    }
                }
            }
        }

        // Parallel fan-out (bounded) when enabled via env TOOL_PARALLELISM>1
        if std::env::var("TOOL_PARALLELISM")
            .ok()
            .and_then(|s| s.parse::<usize>().ok())
            .unwrap_or(1)
            > 1
            && total > 1
        {
            use std::sync::Arc;
            use tokio::sync::Semaphore;

            // Determine parallelism and clamp
            let parallelism = std::env::var("TOOL_PARALLELISM")
                .ok()
                .and_then(|s| s.parse::<usize>().ok())
                .map(|n| n.clamp(1, 32))
                .unwrap_or(1);
            let semaphore = Arc::new(Semaphore::new(parallelism));

            // Pre-parse items and enforce allowlist prior to spawning
            let mut parsed: Vec<(usize, String, HashMap<String, serde_json::Value>)> =
                Vec::with_capacity(total);
            for (idx, item) in list.values.iter().enumerate() {
                let (tool_name, params_map) = match &item.kind {
                    Some(prost_types::value::Kind::StructValue(s)) => {
                        let tool = s
                            .fields
                            .get("tool")
                            .and_then(|v| v.kind.as_ref())
                            .and_then(|k| match k {
                                prost_types::value::Kind::StringValue(s) => Some(s.clone()),
                                _ => None,
                            })
                            .ok_or_else(|| {
                                Status::invalid_argument(
                                    "tool_calls[*] missing 'tool' string field",
                                )
                            })?;
                        if !req.available_tools.is_empty() && !req.available_tools.contains(&tool) {
                            return Err(Status::permission_denied(format!(
                                "Tool '{}' is not allowed for this request",
                                tool
                            )));
                        }
                        if let Some(ref allow) = ctx_allowed {
                            if !allow.contains(&tool) {
                                return Err(Status::permission_denied(format!(
                                    "Tool '{}' is not permitted by context",
                                    tool
                                )));
                            }
                        }
                        let mut params = HashMap::new();
                        if let Some(par_v) = s.fields.get("parameters") {
                            if let Some(prost_types::value::Kind::StructValue(pst)) = &par_v.kind {
                                for (k, v) in &pst.fields {
                                    params.insert(
                                        k.clone(),
                                        crate::grpc_server::prost_value_to_json(v),
                                    );
                                }
                            }
                        }
                        (tool, params)
                    }
                    _ => {
                        return Err(Status::invalid_argument(
                            "tool_calls elements must be objects",
                        ))
                    }
                };
                parsed.push((idx, tool_name, params_map));
            }

            struct ItemRes {
                tool_name: String,
                params_map: HashMap<String, serde_json::Value>,
                success: bool,
                output: serde_json::Value,
                error: String,
                dur_ms: i64,
            }

            // Pre-allocate results without requiring Clone on ItemRes
            let mut results: Vec<Option<ItemRes>> = (0..total).map(|_| None).collect();
            let wall_start = std::time::Instant::now();
            let mut handles = Vec::with_capacity(total);
            for (idx, tool_name, params_map) in parsed.into_iter() {
                let permit = semaphore.clone().acquire_owned().await.map_err(|e| {
                    tonic::Status::internal(format!("Failed to acquire semaphore permit: {}", e))
                })?;
                #[cfg(feature = "wasi")]
                let sandbox = self.sandbox.clone();
                #[cfg(not(feature = "wasi"))]
                let sandbox = ();
                let tool_name_c = tool_name.clone();
                let params_map_c = params_map.clone();
                let context_c = req.context.clone();
                let jh = tokio::spawn(async move {
                    let _p = permit;
                    let exec = ToolExecutor::new_with_wasi(Some(sandbox), None);
                    let call = ToolCall {
                        tool_name: tool_name_c.clone(),
                        parameters: params_map_c.clone(),
                        call_id: None,
                    };
                    let start = std::time::Instant::now();
                    let outcome = exec.execute_tool(&call, context_c.as_ref()).await;
                    let dur_ms = start.elapsed().as_millis() as i64;
                    match outcome {
                        Ok(res) => ItemRes {
                            tool_name: tool_name_c,
                            params_map: params_map_c,
                            success: res.success,
                            output: res.output,
                            error: res.error.unwrap_or_default(),
                            dur_ms,
                        },
                        Err(e) => ItemRes {
                            tool_name: tool_name_c,
                            params_map: params_map_c,
                            success: false,
                            output: serde_json::Value::Null,
                            error: e.to_string(),
                            dur_ms: 0,
                        },
                    }
                });
                handles.push((idx, jh));
            }

            for (idx, jh) in handles {
                match jh.await {
                    Ok(item) => {
                        results[idx] = Some(item);
                    }
                    Err(e) => {
                        results[idx] = Some(ItemRes {
                            tool_name: "unknown".to_string(),
                            params_map: HashMap::new(),
                            success: false,
                            output: serde_json::Value::Null,
                            error: format!("join error: {}", e),
                            dur_ms: 0,
                        });
                    }
                }
            }

            // Build response in original order
            let mut tool_calls_vec = Vec::with_capacity(total);
            let mut tool_results_vec = Vec::with_capacity(total);
            let mut overall_status = proto::common::StatusCode::Ok.into();
            let mut failure_msgs: Vec<String> = Vec::new();
            let mut cumulative_ms: i64 = 0;
            for r in results.into_iter().flatten() {
                cumulative_ms += r.dur_ms;
                if !r.success {
                    overall_status = proto::common::StatusCode::Error.into();
                    if !r.error.is_empty() {
                        failure_msgs.push(format!("{}: {}", r.tool_name, r.error));
                    }
                }
                let tc_params_struct = prost_types::Struct {
                    fields: r
                        .params_map
                        .iter()
                        .map(|(k, v)| {
                            (
                                k.clone(),
                                crate::grpc_server::prost_value_to_json_to_prost(v),
                            )
                        })
                        .collect(),
                };
                let tc = proto::common::ToolCall {
                    name: r.tool_name.clone(),
                    parameters: Some(tc_params_struct),
                    tool_id: r.tool_name.clone(),
                };
                let tr = proto::common::ToolResult {
                    tool_id: r.tool_name.clone(),
                    output: Some(crate::grpc_server::prost_value_to_json_to_prost(&r.output)),
                    status: if r.success {
                        proto::common::StatusCode::Ok.into()
                    } else {
                        proto::common::StatusCode::Error.into()
                    },
                    error_message: r.error,
                    execution_time_ms: r.dur_ms,
                };
                tool_calls_vec.push(tc);
                tool_results_vec.push(tr);
            }

            let result_text = if let Some(last) = tool_results_vec.last() {
                match &last.output {
                    Some(v) => extract_simple_text_from_prost(v),
                    None => String::new(),
                }
            } else {
                String::new()
            };

            let succeeded = overall_status == proto::common::StatusCode::Ok as i32;
            let wall_ms = wall_start.elapsed().as_millis() as i64;
            info!(
                "Multi-tool execution complete: tools={}, sum_ms={}, wall_ms={}, failures={}",
                tool_calls_vec.len(),
                cumulative_ms,
                wall_ms,
                failure_msgs.len()
            );
            let response = ExecuteTaskResponse {
                task_id: req
                    .metadata
                    .as_ref()
                    .map(|m| m.task_id.clone())
                    .unwrap_or_default(),
                status: overall_status,
                result: result_text,
                tool_calls: tool_calls_vec,
                tool_results: tool_results_vec,
                metrics: Some(proto::common::ExecutionMetrics {
                    latency_ms: cumulative_ms,
                    token_usage: None,
                    cache_hit: false,
                    cache_score: 0.0,
                    agents_used: 0,
                    mode: req.mode,
                }),
                error_message: if failure_msgs.is_empty() {
                    String::new()
                } else {
                    failure_msgs.join("; ")
                },
                final_state: if succeeded {
                    proto::agent::AgentState::Completed.into()
                } else {
                    proto::agent::AgentState::Failed.into()
                },
            };
            tracing::info!(
                "ExecuteTaskResponse (multi-tool): token_usage=None, tools={}, cumulative_ms={}",
                response.tool_calls.len(),
                cumulative_ms
            );
            return Ok(Response::new(response));
        }
        for (idx, item) in list.values.iter().enumerate() {
            let (tool_name, params_map) = match &item.kind {
                Some(prost_types::value::Kind::StructValue(s)) => {
                    let tool = s
                        .fields
                        .get("tool")
                        .and_then(|v| v.kind.as_ref())
                        .and_then(|k| match k {
                            prost_types::value::Kind::StringValue(s) => Some(s.clone()),
                            _ => None,
                        })
                        .ok_or_else(|| {
                            Status::invalid_argument("tool_calls[*] missing 'tool' string field")
                        })?;

                    if !req.available_tools.is_empty() && !req.available_tools.contains(&tool) {
                        return Err(Status::permission_denied(format!(
                            "Tool '{}' is not allowed for this request",
                            tool
                        )));
                    }
                    if let Some(ref allow) = ctx_allowed {
                        if !allow.contains(&tool) {
                            return Err(Status::permission_denied(format!(
                                "Tool '{}' is not permitted by context",
                                tool
                            )));
                        }
                    }

                    let mut params = HashMap::new();
                    if let Some(par_v) = s.fields.get("parameters") {
                        if let Some(prost_types::value::Kind::StructValue(pst)) = &par_v.kind {
                            for (k, v) in &pst.fields {
                                params.insert(k.clone(), to_json(v));
                            }
                        }
                    }
                    (tool, params)
                }
                _ => {
                    return Err(Status::invalid_argument(
                        "tool_calls elements must be objects",
                    ))
                }
            };

            let call = ToolCall {
                tool_name: tool_name.clone(),
                parameters: params_map.clone(),
                call_id: None,
            };
            debug!("Executing tool {}/{}: {}", idx + 1, total, tool_name);
            let start = std::time::Instant::now();
            match tool_executor
                .execute_tool(&call, req.context.as_ref())
                .await
            {
                Ok(res) => {
                    let dur = start.elapsed().as_millis() as i64;
                    last_output = res.output.clone();
                    cumulative_ms += dur;
                    if !res.success {
                        overall_status = proto::common::StatusCode::Error.into();
                        if let Some(err) = &res.error {
                            failure_msgs.push(format!("{}: {}", tool_name, err));
                        }
                    }

                    // Build response artifacts
                    let tc_params_struct = prost_types::Struct {
                        fields: params_map
                            .iter()
                            .map(|(k, v)| {
                                (
                                    k.clone(),
                                    crate::grpc_server::prost_value_to_json_to_prost(v),
                                )
                            })
                            .collect(),
                    };
                    let tc = proto::common::ToolCall {
                        name: tool_name.clone(),
                        parameters: Some(tc_params_struct),
                        tool_id: tool_name.clone(),
                    };
                    let tr = proto::common::ToolResult {
                        tool_id: tool_name.clone(),
                        output: Some(crate::grpc_server::prost_value_to_json_to_prost(
                            &res.output,
                        )),
                        status: if res.success {
                            proto::common::StatusCode::Ok.into()
                        } else {
                            proto::common::StatusCode::Error.into()
                        },
                        error_message: res.error.clone().unwrap_or_default(),
                        execution_time_ms: dur,
                    };
                    tool_calls_vec.push(tc);
                    tool_results_vec.push(tr);
                }
                Err(e) => {
                    let dur = start.elapsed().as_millis() as i64;
                    overall_status = proto::common::StatusCode::Error.into();
                    failure_msgs.push(format!("{}: {}", tool_name, e));
                    let tc_params_struct = prost_types::Struct {
                        fields: params_map
                            .iter()
                            .map(|(k, v)| {
                                (
                                    k.clone(),
                                    crate::grpc_server::prost_value_to_json_to_prost(v),
                                )
                            })
                            .collect(),
                    };
                    let tc = proto::common::ToolCall {
                        name: tool_name.clone(),
                        parameters: Some(tc_params_struct),
                        tool_id: tool_name.clone(),
                    };
                    let tr = proto::common::ToolResult {
                        tool_id: tool_name.clone(),
                        output: None,
                        status: proto::common::StatusCode::Error.into(),
                        error_message: e.to_string(),
                        execution_time_ms: dur,
                    };
                    tool_calls_vec.push(tc);
                    tool_results_vec.push(tr);
                }
            }
        }

        let succeeded = overall_status == proto::common::StatusCode::Ok as i32;
        info!(
            "Multi-tool execution complete: tools={}, total_ms={}, failures={}",
            tool_calls_vec.len(),
            cumulative_ms,
            failure_msgs.len()
        );
        let response = ExecuteTaskResponse {
            task_id: req
                .metadata
                .as_ref()
                .map(|m| m.task_id.clone())
                .unwrap_or_default(),
            status: overall_status,
            result: extract_simple_text_from_json(&last_output),
            tool_calls: tool_calls_vec,
            tool_results: tool_results_vec,
            metrics: Some(proto::common::ExecutionMetrics {
                latency_ms: cumulative_ms,
                token_usage: None,
                cache_hit: false,
                cache_score: 0.0,
                agents_used: 0,
                mode: req.mode,
            }),
            error_message: if failure_msgs.is_empty() {
                String::new()
            } else {
                failure_msgs.join("; ")
            },
            final_state: if succeeded {
                proto::agent::AgentState::Completed.into()
            } else {
                proto::agent::AgentState::Failed.into()
            },
        };
        Ok(Response::new(response))
    }
}

#[tonic::async_trait]
impl AgentService for AgentServiceImpl {
    async fn execute_task(
        &self,
        request: Request<ExecuteTaskRequest>,
    ) -> Result<Response<ExecuteTaskResponse>, Status> {
        let req = request.into_inner();
        info!("Executing task: {}", req.query);

        // Validate sandbox permissions early (non-fatal). Ensures WASI sandbox wiring is active.
        #[cfg(feature = "wasi")]
        if let Err(e) = self.sandbox.validate_permissions() {
            tracing::warn!("WASI sandbox permission validation warning: {}", e);
        }

        // Extract session context if available
        if let Some(session_ctx) = &req.session_context {
            info!(
                "Processing with session context: session_id={}",
                session_ctx.session_id
            );
            debug!("Session history length: {}", session_ctx.history.len());
        }

        // Configure sandbox from request config (timeouts, memory)
        // Note: currently unused in this code path; kept for future wiring.
        #[cfg(feature = "wasi")]
        let mut _sandbox = self.sandbox.clone();
        #[cfg(not(feature = "wasi"))]
        let mut _sandbox = ();
        #[cfg(feature = "wasi")]
        if let Some(cfg) = &req.config {
            if cfg.timeout_seconds > 0 {
                _sandbox = _sandbox.set_execution_timeout(std::time::Duration::from_secs(
                    cfg.timeout_seconds as u64,
                ));
            }
            if cfg.memory_limit_mb > 0 {
                _sandbox = _sandbox.set_memory_limit((cfg.memory_limit_mb as usize) * 1024 * 1024);
            }
        }
        #[cfg(not(feature = "wasi"))]
        let _ = &req.config; // Avoid unused variable warning

        // Check for multi-tool sequence first
        info!("Request context: {:?}", req.context);
        if let Some(context) = &req.context {
            info!(
                "Context fields available: {:?}",
                context.fields.keys().collect::<Vec<_>>()
            );
            if let Some(tc_list) = context.fields.get("tool_calls") {
                if let Some(prost_types::value::Kind::ListValue(list)) = &tc_list.kind {
                    info!("Detected tool_calls list; executing (parallelism may apply)");
                    let key = req
                        .metadata
                        .as_ref()
                        .map(|m| m.user_id.clone())
                        .unwrap_or_else(|| "anonymous".to_string());
                    let est = req.query.len().saturating_div(4);
                    let enforcer = self.enforcer.clone();
                    let list = list.clone();
                    let result = enforcer
                        .enforce(&key, est, || async {
                            self.execute_tool_calls(&list, &req)
                                .await
                                .map_err(|e| anyhow::anyhow!(e.to_string()))
                        })
                        .await;
                    return result.map_err(|e| match e.to_string().as_str() {
                        "request_timeout" => Status::deadline_exceeded("timeout"),
                        "rate_limit_exceeded" => Status::resource_exhausted("rate_limited"),
                        "circuit_breaker_open" => Status::unavailable("circuit_open"),
                        "token_limit_exceeded" => Status::resource_exhausted("token_limit"),
                        _ => Status::internal(e.to_string()),
                    });
                }
            }
            // ENFORCEMENT: Check for tool parameters and execute tools directly
            if let Some(tool_params) = context.fields.get("tool_parameters") {
                info!(
                    "ENFORCING tool execution from orchestration parameters - bypassing LLM choice"
                );
                let key = req
                    .metadata
                    .as_ref()
                    .map(|m| m.user_id.clone())
                    .unwrap_or_else(|| "anonymous".to_string());
                let est = req.query.len().saturating_div(4);
                let enforcer = self.enforcer.clone();
                let tp = tool_params.clone();
                let result = enforcer
                    .enforce(&key, est, || async {
                        self.execute_direct_tool(&tp, &req)
                            .await
                            .map_err(|e| anyhow::anyhow!(e.to_string()))
                    })
                    .await;
                return result.map_err(|e| match e.to_string().as_str() {
                    "request_timeout" => Status::deadline_exceeded("timeout"),
                    "rate_limit_exceeded" => Status::resource_exhausted("rate_limited"),
                    "circuit_breaker_open" => Status::unavailable("circuit_open"),
                    "token_limit_exceeded" => Status::resource_exhausted("token_limit"),
                    _ => Status::internal(e.to_string()),
                });
            }
        }

        // Check if we have suggested tools from decomposition
        let has_suggested_tools = !req.available_tools.is_empty();

        // Simple/Standard mode -> Use tool-enabled LLM if tools suggested, otherwise direct LLM
        match req.mode() {
            proto::common::ExecutionMode::Simple | proto::common::ExecutionMode::Standard => {
                if has_suggested_tools {
                    info!("Tool-enabled LLM query path for simple/standard with suggested tools: {:?}", req.available_tools);
                } else {
                    info!("Direct LLM query path (no tools) for simple/standard");
                }

                let llm = self.llm.clone();

                // Merge minimal context (history to summary string if present)
                let mut ctx_json = serde_json::json!({});
                if let Some(ctx) = &req.context {
                    // pass-through Struct -> serde_json
                    let mut map = serde_json::Map::new();
                    for (k, v) in &ctx.fields {
                        map.insert(k.clone(), prost_value_to_json(v));
                    }
                    ctx_json = serde_json::Value::Object(map);
                }

                if let Some(session_ctx) = &req.session_context {
                    if !session_ctx.history.is_empty() {
                        let hist = session_ctx.history.join("\n");
                        if let Some(obj) = ctx_json.as_object_mut() {
                            obj.insert("history".to_string(), serde_json::Value::String(hist));
                        }
                    }
                }

                let mode_str = match req.mode() {
                    proto::common::ExecutionMode::Simple => "simple",
                    proto::common::ExecutionMode::Standard => "standard",
                    proto::common::ExecutionMode::Complex => "complex",
                    _ => "standard",
                };
                let key = req
                    .metadata
                    .as_ref()
                    .map(|m| m.user_id.clone())
                    .unwrap_or_else(|| "anonymous".to_string());
                let est = req.query.len().saturating_div(4);
                let enforcer = self.enforcer.clone();
                let q = req.query.clone();
                let ctx = ctx_json.clone();

                // Pass suggested tools to LLM if available
                let tools_option = if has_suggested_tools {
                    Some(req.available_tools.clone())
                } else {
                    None
                };

                let (result, usage) = enforcer
                    .enforce(&key, est, || async {
                        llm.query_agent(&q, "agent-core", mode_str, Some(ctx), tools_option.clone())
                            .await
                            .map_err(|e| anyhow::anyhow!(e.to_string()))
                    })
                    .await
                    .map_err(|e| match e.to_string().as_str() {
                        "request_timeout" => Status::deadline_exceeded("timeout"),
                        "rate_limit_exceeded" => Status::resource_exhausted("rate_limited"),
                        "circuit_breaker_open" => Status::unavailable("circuit_open"),
                        "token_limit_exceeded" => Status::resource_exhausted("token_limit"),
                        _ => Status::internal(e.to_string()),
                    })?;

                // Save values for logging before move
                let log_provider = usage.provider.clone();
                let log_model = usage.model.clone();
                let log_tokens = usage.total_tokens;

                let response = ExecuteTaskResponse {
                    task_id: req
                        .metadata
                        .as_ref()
                        .map(|m| m.task_id.clone())
                        .unwrap_or_default(),
                    status: proto::common::StatusCode::Ok.into(),
                    result,
                    tool_calls: vec![],
                    tool_results: vec![],
                    metrics: Some(proto::common::ExecutionMetrics {
                        latency_ms: 100,
                        token_usage: Some(proto::common::TokenUsage {
                            prompt_tokens: usage.prompt_tokens as i32,
                            completion_tokens: usage.completion_tokens as i32,
                            total_tokens: usage.total_tokens as i32,
                            cost_usd: usage.cost_usd,
                            model: usage.model,
                            provider: usage.provider,
                            tier: match req.mode() {
                                proto::common::ExecutionMode::Simple => {
                                    proto::common::ModelTier::Small.into()
                                }
                                proto::common::ExecutionMode::Complex => {
                                    proto::common::ModelTier::Large.into()
                                }
                                _ => proto::common::ModelTier::Medium.into(),
                            },
                        }),
                        cache_hit: false,
                        cache_score: 0.0,
                        agents_used: 1,
                        mode: req.mode,
                    }),
                    error_message: String::new(),
                    final_state: proto::agent::AgentState::Completed.into(),
                };

                tracing::info!(
                    "ExecuteTaskResponse (LLM): provider={}, model={}, tokens={} (suggested-tools path)",
                    log_provider,
                    log_model,
                    log_tokens
                );
                return Ok(Response::new(response));
            }
            _ => {}
        }

        // Complex or unspecified -> LLM path with enforcement and optional context
        if has_suggested_tools {
            info!(
                "Tool-enabled LLM query path for complex with suggested tools: {:?}",
                req.available_tools
            );
        } else {
            info!("Direct LLM query path (no tools) for complex");
        }

        let llm = self.llm.clone();
        let key = req
            .metadata
            .as_ref()
            .map(|m| m.user_id.clone())
            .unwrap_or_else(|| "anonymous".to_string());
        let est = req.query.len().saturating_div(4);
        let enforcer = self.enforcer.clone();
        let mode_str = match req.mode() {
            proto::common::ExecutionMode::Complex => "complex",
            proto::common::ExecutionMode::Simple => "simple",
            _ => "standard",
        };
        // Build minimal context from session history
        let mut ctx_json = serde_json::json!({});
        if let Some(session_ctx) = &req.session_context {
            if !session_ctx.history.is_empty() {
                let hist = session_ctx.history.join("\n");
                if let Some(obj) = ctx_json.as_object_mut() {
                    obj.insert("history".to_string(), serde_json::Value::String(hist));
                }
            }
        }
        let q = req.query.clone();

        // Pass suggested tools to LLM for complex mode as well
        let tools_option = if has_suggested_tools {
            Some(req.available_tools.clone())
        } else {
            None
        };

        let (result, usage) = enforcer
            .enforce(&key, est, || async {
                llm.query_agent(
                    &q,
                    "agent-core",
                    mode_str,
                    Some(ctx_json),
                    tools_option.clone(),
                )
                .await
                .map_err(|e| anyhow::anyhow!(e.to_string()))
            })
            .await
            .map_err(|e| match e.to_string().as_str() {
                "request_timeout" => Status::deadline_exceeded("timeout"),
                "rate_limit_exceeded" => Status::resource_exhausted("rate_limited"),
                "circuit_breaker_open" => Status::unavailable("circuit_open"),
                "token_limit_exceeded" => Status::resource_exhausted("token_limit"),
                _ => Status::internal(e.to_string()),
            })?;

        // Save values for logging before move
        let log_provider = usage.provider.clone();
        let log_model = usage.model.clone();
        let log_tokens = usage.total_tokens;

        let response = ExecuteTaskResponse {
            task_id: req
                .metadata
                .as_ref()
                .map(|m| m.task_id.clone())
                .unwrap_or_default(),
            status: proto::common::StatusCode::Ok.into(),
            result,
            tool_calls: vec![],
            tool_results: vec![],
            metrics: Some(proto::common::ExecutionMetrics {
                latency_ms: 100,
                token_usage: Some(proto::common::TokenUsage {
                    prompt_tokens: usage.prompt_tokens as i32,
                    completion_tokens: usage.completion_tokens as i32,
                    total_tokens: usage.total_tokens as i32,
                    cost_usd: usage.cost_usd,
                    model: usage.model,
                    provider: usage.provider,
                    tier: match req.mode() {
                        proto::common::ExecutionMode::Simple => {
                            proto::common::ModelTier::Small.into()
                        }
                        proto::common::ExecutionMode::Complex => {
                            proto::common::ModelTier::Large.into()
                        }
                        _ => proto::common::ModelTier::Medium.into(),
                    },
                }),
                cache_hit: false,
                cache_score: 0.0,
                agents_used: 1,
                mode: req.mode,
            }),
            error_message: String::new(),
            final_state: proto::agent::AgentState::Completed.into(),
        };

        tracing::info!(
            "ExecuteTaskResponse (LLM final): provider={}, model={}, tokens={}",
            log_provider,
            log_model,
            log_tokens
        );
        Ok(Response::new(response))
    }

    async fn stream_execute_task(
        &self,
        request: Request<ExecuteTaskRequest>,
    ) -> Result<Response<Self::StreamExecuteTaskStream>, Status> {
        let req = request.into_inner();
        info!("Stream executing task: {}", req.query);

        let (tx, rx) = mpsc::channel(128);

        let task_id = req
            .metadata
            .as_ref()
            .map(|m| m.task_id.clone())
            .unwrap_or_else(|| "stream-task".to_string());
        let llm = self.llm.clone();

        // Build minimal JSON context (including session history)
        let mut ctx_json = serde_json::json!({});
        if let Some(ctx) = &req.context {
            let mut map = serde_json::Map::new();
            for (k, v) in &ctx.fields {
                map.insert(k.clone(), prost_value_to_json(v));
            }
            ctx_json = serde_json::Value::Object(map);
        }
        if let Some(session_ctx) = &req.session_context {
            if !session_ctx.history.is_empty() {
                let hist = session_ctx.history.join("\n");
                if let Some(obj) = ctx_json.as_object_mut() {
                    obj.insert("history".to_string(), serde_json::Value::String(hist));
                }
            }
        }

        let mode_str = match req.mode() {
            proto::common::ExecutionMode::Simple => "simple",
            proto::common::ExecutionMode::Complex => "complex",
            _ => "standard",
        };

        tokio::spawn(async move {
            let _ = tx
                .send(Ok(TaskUpdate {
                    task_id: task_id.clone(),
                    state: proto::agent::AgentState::Planning.into(),
                    message: "Starting task execution".to_string(),
                    tool_call: None,
                    tool_result: None,
                    progress: 0.0,
                    delta: String::new(),
                }))
                .await;

            let tools_option = if req.available_tools.is_empty() {
                None
            } else {
                Some(req.available_tools.clone())
            };

            match llm
                .stream_query_agent(
                    &req.query,
                    "agent-core",
                    mode_str,
                    Some(ctx_json),
                    tools_option,
                )
                .await
            {
                Ok(mut stream) => {
                    let mut buffer = String::new();
                    while let Some(item) = stream.next().await {
                        match item {
                            Ok(chunk) => {
                                if let Some(d) = chunk.delta.clone() {
                                    buffer.push_str(&d);
                                    let _ = tx
                                        .send(Ok(TaskUpdate {
                                            task_id: task_id.clone(),
                                            state: proto::agent::AgentState::Executing.into(),
                                            message: String::new(),
                                            tool_call: None,
                                            tool_result: None,
                                            progress: 0.0,
                                            delta: d,
                                        }))
                                        .await;
                                }

                                if let Some(final_msg) = chunk.final_message {
                                    let final_text = if final_msg.response.is_empty() {
                                        buffer.clone()
                                    } else {
                                        final_msg.response
                                    };
                                    // Attach usage metadata when available so downstream can track budgets
                                    let usage_result = {
                                        let has_usage = final_msg.total_tokens.is_some()
                                            || final_msg.input_tokens.is_some()
                                            || final_msg.output_tokens.is_some()
                                            || final_msg.cost_usd.is_some()
                                            || final_msg.model_used.is_some()
                                            || final_msg.provider.is_some();
                                        if has_usage {
                                            let usage_json = serde_json::json!({
                                                "total_tokens": final_msg.total_tokens,
                                                "input_tokens": final_msg.input_tokens,
                                                "output_tokens": final_msg.output_tokens,
                                                "cost_usd": final_msg.cost_usd,
                                                "model": final_msg.model_used.clone().unwrap_or_default(),
                                                "provider": final_msg.provider.clone().unwrap_or_default(),
                                            });
                                            Some(proto::common::ToolResult {
                                                tool_id: "usage_metrics".to_string(),
                                                output: Some(crate::grpc_server::prost_value_to_json_to_prost(
                                                    &usage_json,
                                                )),
                                                status: proto::common::StatusCode::Ok.into(),
                                                error_message: String::new(),
                                                execution_time_ms: 0,
                                            })
                                        } else {
                                            None
                                        }
                                    };
                                    let _ = tx
                                        .send(Ok(TaskUpdate {
                                            task_id: task_id.clone(),
                                            state: proto::agent::AgentState::Completed.into(),
                                            message: final_text,
                                            tool_call: None,
                                            tool_result: usage_result,
                                            progress: 1.0,
                                            delta: String::new(),
                                        }))
                                        .await;
                                    return;
                                }
                            }
                            Err(e) => {
                                let _ = tx.send(Err(Status::internal(e.to_string()))).await;
                                return;
                            }
                        }
                    }

                    // If stream ends without an explicit final chunk, emit completion with buffered text
                    let _ = tx
                        .send(Ok(TaskUpdate {
                            task_id: task_id.clone(),
                            state: proto::agent::AgentState::Completed.into(),
                            message: buffer,
                            tool_call: None,
                            tool_result: None,
                            progress: 1.0,
                            delta: String::new(),
                        }))
                        .await;
                }
                Err(e) => {
                    let _ = tx.send(Err(Status::internal(e.to_string()))).await;
                }
            }
        });

        Ok(Response::new(
            tokio_stream::wrappers::ReceiverStream::new(rx) as Self::StreamExecuteTaskStream,
        ))
    }

    type StreamExecuteTaskStream =
        tokio_stream::wrappers::ReceiverStream<Result<TaskUpdate, Status>>;

    async fn get_capabilities(
        &self,
        _request: Request<GetCapabilitiesRequest>,
    ) -> Result<Response<GetCapabilitiesResponse>, Status> {
        debug!("Getting agent capabilities");

        let response = GetCapabilitiesResponse {
            supported_tools: vec![
                "web_search".to_string(),
                "code_executor".to_string(),
                "database_query".to_string(),
            ],
            supported_modes: vec![
                proto::common::ExecutionMode::Simple.into(),
                proto::common::ExecutionMode::Standard.into(),
                proto::common::ExecutionMode::Complex.into(),
            ],
            max_memory_mb: 512,
            max_concurrent_tasks: 10,
            version: env!("CARGO_PKG_VERSION").to_string(),
        };

        Ok(Response::new(response))
    }

    async fn health_check(
        &self,
        _request: Request<HealthCheckRequest>,
    ) -> Result<Response<HealthCheckResponse>, Status> {
        debug!("Health check requested");

        let (current_memory, max_memory) = self.memory_pool.get_usage_stats().await;
        let memory_usage_percent = (current_memory as f64 / max_memory as f64) * 100.0;

        let response = HealthCheckResponse {
            healthy: true,
            message: "Agent core is healthy".to_string(),
            uptime_seconds: self.start_time.elapsed().as_secs() as i64,
            active_tasks: 0, // Would track this in production
            memory_usage_percent,
        };

        Ok(Response::new(response))
    }

    async fn discover_tools(
        &self,
        _request: Request<DiscoverToolsRequest>,
    ) -> Result<Response<DiscoverToolsResponse>, Status> {
        debug!("Tool discovery requested");

        let response = DiscoverToolsResponse {
            tools: vec![], // Stub implementation
        };

        Ok(Response::new(response))
    }

    async fn get_tool_capability(
        &self,
        _request: Request<GetToolCapabilityRequest>,
    ) -> Result<Response<GetToolCapabilityResponse>, Status> {
        debug!("Tool capability requested");

        let response = GetToolCapabilityResponse {
            tool: None, // Stub implementation
        };

        Ok(Response::new(response))
    }
}

// Helper: convert prost_types::Value to serde_json::Value for passing context to Python
pub fn prost_value_to_json(v: &prost_types::Value) -> serde_json::Value {
    use prost_types::value::Kind::*;
    match v.kind.as_ref() {
        Some(NullValue(_)) => serde_json::Value::Null,
        Some(BoolValue(b)) => serde_json::Value::Bool(*b),
        Some(NumberValue(n)) => serde_json::json!(*n),
        Some(StringValue(s)) => serde_json::Value::String(s.clone()),
        Some(ListValue(lv)) => {
            serde_json::Value::Array(lv.values.iter().map(prost_value_to_json).collect())
        }
        Some(StructValue(st)) => {
            let mut map = serde_json::Map::new();
            for (k, v) in &st.fields {
                map.insert(k.clone(), prost_value_to_json(v));
            }
            serde_json::Value::Object(map)
        }
        None => serde_json::Value::Null,
    }
}

// Helper: convert serde_json::Value back to prost_types::Value
fn prost_value_to_json_to_prost(v: &serde_json::Value) -> prost_types::Value {
    use prost_types::value::Kind;
    match v {
        serde_json::Value::Null => prost_types::Value {
            kind: Some(Kind::NullValue(0)),
        },
        serde_json::Value::Bool(b) => prost_types::Value {
            kind: Some(Kind::BoolValue(*b)),
        },
        serde_json::Value::Number(n) => prost_types::Value {
            kind: Some(Kind::NumberValue(n.as_f64().unwrap_or(0.0))),
        },
        serde_json::Value::String(s) => prost_types::Value {
            kind: Some(Kind::StringValue(s.clone())),
        },
        serde_json::Value::Array(arr) => prost_types::Value {
            kind: Some(Kind::ListValue(prost_types::ListValue {
                values: arr.iter().map(prost_value_to_json_to_prost).collect(),
            })),
        },
        serde_json::Value::Object(map) => prost_types::Value {
            kind: Some(Kind::StructValue(prost_types::Struct {
                fields: map
                    .iter()
                    .map(|(k, v)| (k.clone(), prost_value_to_json_to_prost(v)))
                    .collect(),
            })),
        },
    }
}

// Helper: produce a simple, user-facing string from a serde_json::Value.
// - If it's a primitive (string/number/bool), stringify it directly
// - If it's an object with a "result" field, surface that field (recursively)
// - If null/array/other objects, fall back to compact JSON string or empty
fn extract_simple_text_from_json(v: &serde_json::Value) -> String {
    match v {
        serde_json::Value::Null => String::new(),
        serde_json::Value::Bool(b) => b.to_string(),
        serde_json::Value::Number(n) => n.to_string(),
        serde_json::Value::String(s) => s.clone(),
        serde_json::Value::Array(_) => v.to_string(),
        serde_json::Value::Object(map) => {
            if let Some(inner) = map.get("result") {
                return extract_simple_text_from_json(inner);
            }
            v.to_string()
        }
    }
}

// Helper: same as above, but starting from prost_types::Value
fn extract_simple_text_from_prost(v: &prost_types::Value) -> String {
    let json = prost_value_to_json(v);
    extract_simple_text_from_json(&json)
}
