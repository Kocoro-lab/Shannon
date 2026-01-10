//! Run lifecycle management.

use std::collections::HashMap;
use std::sync::Arc;

use futures::StreamExt;
use parking_lot::RwLock;
use tokio::sync::broadcast;
use uuid::Uuid;

use crate::domain::{Run, RunStatus, Session};
use crate::events::{NormalizedEvent, StreamEvent};
use crate::llm::orchestrator::Orchestrator;
use crate::llm::Message;
use crate::logging::OpTimer;

/// Event channel capacity.
const EVENT_CHANNEL_CAPACITY: usize = 256;

/// Manages active runs and their lifecycle.
#[derive(Clone)]
pub struct RunManager {
    /// Active runs by ID.
    active_runs: Arc<RwLock<HashMap<String, RunState>>>,
    /// Session store.
    sessions: Arc<RwLock<HashMap<String, Session>>>,
    /// LLM orchestrator.
    orchestrator: Arc<Orchestrator>,
}

impl std::fmt::Debug for RunManager {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let runs = self.active_runs.read();
        f.debug_struct("RunManager")
            .field("active_runs", &runs.keys().collect::<Vec<_>>())
            .finish()
    }
}

/// Internal state for an active run.
struct RunState {
    run: Run,
    sender: broadcast::Sender<StreamEvent>,
}

impl RunManager {
    /// Create a new run manager.
    pub fn new(orchestrator: Arc<Orchestrator>) -> Self {
        Self {
            active_runs: Arc::new(RwLock::new(HashMap::new())),
            sessions: Arc::new(RwLock::new(HashMap::new())),
            orchestrator,
        }
    }

    /// Start a new run.
    pub async fn start_run(
        &self,
        query: impl Into<String>,
        session_id: Option<String>,
        user_id: Option<String>,
    ) -> anyhow::Result<(String, broadcast::Receiver<StreamEvent>)> {
        let timer = OpTimer::new("run_manager", "start_run");
        let query = query.into();
        
        let query_preview = if query.len() > 100 {
            format!("{}...", &query[..100])
        } else {
            query.clone()
        };
        
        tracing::info!(
            "🎬 Starting new run - query_len={}, session_id={:?}, user_id={:?}",
            query.len(),
            session_id,
            user_id
        );
        tracing::debug!("📝 Run query preview: {}", query_preview);
        
        // Create run
        let mut run = Run::new(&query);
        if let Some(ref sid) = session_id {
            run = run.with_session(sid.clone());
            tracing::trace!("📋 Linked to session - session_id={}", sid);
        }
        if let Some(ref uid) = user_id {
            run = run.with_user(uid.clone());
            tracing::trace!("👤 Linked to user - user_id={}", uid);
        }
        run.start();

        let run_id = run.id.clone();
        
        tracing::debug!("✅ Run created - run_id={}", run_id);

        // Create event channel
        let (sender, receiver) = broadcast::channel(EVENT_CHANNEL_CAPACITY);
        tracing::trace!("📡 Event channel created - capacity={}", EVENT_CHANNEL_CAPACITY);

        // Store run state
        {
            let mut runs = self.active_runs.write();
            runs.insert(run_id.clone(), RunState {
                run,
                sender: sender.clone(),
            });
            tracing::trace!("📦 Run registered - run_id={}, active_count={}", run_id, runs.len());
        }

        // Get or create session
        let session = self.get_or_create_session(session_id.clone());
        tracing::debug!(
            "📋 Session ready - session_id={}, message_count={}",
            session.id,
            session.messages.len()
        );

        // Build messages from session history
        let mut messages = session.messages.clone();
        messages.push(Message::user(&query));
        
        tracing::debug!(
            "💬 Messages prepared - total_count={}, history_count={}",
            messages.len(),
            session.messages.len()
        );

        // Spawn task to execute the run
        let orchestrator = self.orchestrator.clone();
        let active_runs = self.active_runs.clone();
        let sessions = self.sessions.clone();
        let run_id_clone = run_id.clone();
        let session_id_clone = session.id.clone();

        tokio::spawn(async move {
            tracing::debug!("🚀 Spawned execution task - run_id={}", run_id_clone);
            
            let result = Self::execute_run(
                orchestrator,
                active_runs.clone(),
                sessions.clone(),
                run_id_clone.clone(),
                session_id_clone,
                messages,
                sender,
            ).await;

            if let Err(e) = result {
                tracing::error!("❌ Run execution failed - run_id={}, error={}", run_id_clone, e);
            }
        });

        timer.finish();
        
        tracing::info!("✅ Run started successfully - run_id={}", run_id);

        Ok((run_id, receiver))
    }

    /// Start a new run with a specific ID.
    ///
    /// Used when the task ID is already generated (e.g., from gateway task submission).
    pub async fn start_run_with_id(
        &self,
        run_id: String,
        query: impl Into<String>,
        session_id: Option<String>,
        user_id: Option<String>,
    ) -> anyhow::Result<broadcast::Receiver<StreamEvent>> {
        let timer = OpTimer::new("run_manager", "start_run_with_id");
        let query = query.into();
        
        let query_preview = if query.len() > 100 {
            format!("{}...", &query[..100])
        } else {
            query.clone()
        };

        tracing::info!(
            "🎬 Starting new run with ID - run_id={}, query_len={}, session_id={:?}, user_id={:?}",
            run_id,
            query.len(),
            session_id,
            user_id
        );
        tracing::debug!("📝 Run query preview: {}", query_preview);

        // Create run with specific ID
        let mut run = Run::new(&query);
        run.id = run_id.clone();
        if let Some(ref sid) = session_id {
            run = run.with_session(sid.clone());
            tracing::trace!("📋 Linked to session - session_id={}", sid);
        }
        if let Some(ref uid) = user_id {
            run = run.with_user(uid.clone());
            tracing::trace!("👤 Linked to user - user_id={}", uid);
        }
        run.start();
        
        tracing::debug!("✅ Run created with specific ID - run_id={}", run_id);

        // Create event channel
        let (sender, receiver) = broadcast::channel(EVENT_CHANNEL_CAPACITY);
        tracing::trace!("📡 Event channel created - capacity={}", EVENT_CHANNEL_CAPACITY);

        // Store run state
        {
            let mut runs = self.active_runs.write();
            runs.insert(run_id.clone(), RunState {
                run,
                sender: sender.clone(),
            });
            tracing::trace!("📦 Run registered - run_id={}, active_count={}", run_id, runs.len());
        }

        // Get or create session
        let session = self.get_or_create_session(session_id.clone());
        tracing::debug!(
            "📋 Session ready - session_id={}, message_count={}",
            session.id,
            session.messages.len()
        );

        // Build messages from session history
        let mut messages = session.messages.clone();
        messages.push(Message::user(&query));
        
        tracing::debug!(
            "💬 Messages prepared - total_count={}, history_count={}",
            messages.len(),
            session.messages.len()
        );

        // Spawn task to execute the run
        let orchestrator = self.orchestrator.clone();
        let active_runs = self.active_runs.clone();
        let sessions = self.sessions.clone();
        let run_id_clone = run_id.clone();
        let session_id_clone = session.id.clone();

        tokio::spawn(async move {
            tracing::debug!("🚀 Spawned execution task - run_id={}", run_id_clone);
            
            let result = Self::execute_run(
                orchestrator,
                active_runs.clone(),
                sessions.clone(),
                run_id_clone.clone(),
                session_id_clone,
                messages,
                sender,
            )
            .await;

            if let Err(e) = result {
                tracing::error!("❌ Run execution failed - run_id={}, error={}", run_id_clone, e);
            }
        });

        timer.finish();
        
        tracing::info!("✅ Run started successfully with ID - run_id={}", run_id);

        Ok(receiver)
    }

    /// Execute the run.
    async fn execute_run(
        orchestrator: Arc<Orchestrator>,
        active_runs: Arc<RwLock<HashMap<String, RunState>>>,
        sessions: Arc<RwLock<HashMap<String, Session>>>,
        run_id: String,
        session_id: String,
        messages: Vec<Message>,
        sender: broadcast::Sender<StreamEvent>,
    ) -> anyhow::Result<()> {
        let timer = OpTimer::new("run_manager", "execute_run");
        
        tracing::info!(
            "⚡ Executing run - run_id={}, session_id={}, message_count={}",
            run_id,
            session_id,
            messages.len()
        );
        
        let mut content_buffer = String::new();
        let mut total_tokens = 0u32;
        let mut chunk_count = 0u32;

        let result: anyhow::Result<()> = async {
            // Stream the response
            tracing::debug!("📞 Calling LLM orchestrator - run_id={}", run_id);
            
            let orchestrator_timer = OpTimer::new("llm_orchestrator", "chat");
            let stream = orchestrator.chat(messages).await?;
            orchestrator_timer.finish();
            
            tracing::debug!("📡 Streaming LLM response - run_id={}", run_id);
            
            futures::pin_mut!(stream);

            while let Some(event) = stream.next().await {
                chunk_count += 1;
                
                // Collect content for session storage
                if let NormalizedEvent::MessageDelta { ref content, .. } = event.event {
                    content_buffer.push_str(content);
                    tracing::trace!(
                        "📝 Received content chunk - run_id={}, chunk={}, size={}",
                        run_id,
                        chunk_count,
                        content.len()
                    );
                }

                // Collect usage
                if let NormalizedEvent::Usage { total_tokens: tokens, .. } = event.event {
                    total_tokens = tokens;
                    tracing::debug!(
                        "📊 Token usage received - run_id={}, total_tokens={}",
                        run_id,
                        tokens
                    );
                }

                // Forward event
                let _ = sender.send(event);
            }

            tracing::info!(
                "✅ Streaming complete - run_id={}, chunks={}, content_len={}, tokens={}",
                run_id,
                chunk_count,
                content_buffer.len(),
                total_tokens
            );

            // Update session with assistant response
            tracing::debug!("💾 Updating session - session_id={}", session_id);
            {
                let mut sessions = sessions.write();
                if let Some(session) = sessions.get_mut(&session_id) {
                    session.add_message(Message::assistant(&content_buffer));
                    session.add_tokens(total_tokens);
                    tracing::trace!(
                        "✅ Session updated - session_id={}, total_messages={}, total_tokens={}",
                        session_id,
                        session.messages.len(),
                        session.total_tokens
                    );
                }
            }

            // Complete the run
            tracing::debug!("📦 Completing run - run_id={}", run_id);
            {
                let mut runs = active_runs.write();
                if let Some(state) = runs.get_mut(&run_id) {
                    state.run.complete(&content_buffer);
                    state.run.add_tokens(total_tokens, 0.0); // Cost calculation would go here
                    tracing::trace!(
                        "✅ Run completed - run_id={}, status={:?}",
                        run_id,
                        state.run.status
                    );
                }
            }

            Ok(())
        }.await;

        timer.finish_with_result(result.as_ref());
        
        if result.is_ok() {
            tracing::info!(
                "✅ Run execution complete - run_id={}, chunks={}, tokens={}",
                run_id,
                chunk_count,
                total_tokens
            );
        }
        
        result
    }

    /// Get or create a session.
    fn get_or_create_session(&self, session_id: Option<String>) -> Session {
        let mut sessions = self.sessions.write();
        
        let id = session_id.unwrap_or_else(|| Uuid::new_v4().to_string());
        
        sessions.entry(id.clone())
            .or_insert_with(|| Session::with_id(id))
            .clone()
    }

    /// Get a run by ID.
    pub fn get_run(&self, run_id: &str) -> Option<Run> {
        let runs = self.active_runs.read();
        runs.get(run_id).map(|s| s.run.clone())
    }

    /// Subscribe to a run's events.
    pub fn subscribe(&self, run_id: &str) -> Option<broadcast::Receiver<StreamEvent>> {
        let runs = self.active_runs.read();
        runs.get(run_id).map(|s| s.sender.subscribe())
    }

    /// Cancel a run.
    pub fn cancel_run(&self, run_id: &str) -> bool {
        let mut runs = self.active_runs.write();
        if let Some(state) = runs.get_mut(run_id) {
            state.run.cancel();
            true
        } else {
            false
        }
    }

    /// Get a session by ID.
    pub fn get_session(&self, session_id: &str) -> Option<Session> {
        let sessions = self.sessions.read();
        sessions.get(session_id).cloned()
    }

    /// List active runs.
    pub fn list_active_runs(&self) -> Vec<Run> {
        let runs = self.active_runs.read();
        runs.values()
            .filter(|s| s.run.status == RunStatus::Running)
            .map(|s| s.run.clone())
            .collect()
    }
}
