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
        let query = query.into();
        
        // Create run
        let mut run = Run::new(&query);
        if let Some(ref sid) = session_id {
            run = run.with_session(sid.clone());
        }
        if let Some(ref uid) = user_id {
            run = run.with_user(uid.clone());
        }
        run.start();

        let run_id = run.id.clone();

        // Create event channel
        let (sender, receiver) = broadcast::channel(EVENT_CHANNEL_CAPACITY);

        // Store run state
        {
            let mut runs = self.active_runs.write();
            runs.insert(run_id.clone(), RunState {
                run,
                sender: sender.clone(),
            });
        }

        // Get or create session
        let session = self.get_or_create_session(session_id);

        // Build messages from session history
        let mut messages = session.messages.clone();
        messages.push(Message::user(&query));

        // Spawn task to execute the run
        let orchestrator = self.orchestrator.clone();
        let active_runs = self.active_runs.clone();
        let sessions = self.sessions.clone();
        let run_id_clone = run_id.clone();
        let session_id_clone = session.id.clone();

        tokio::spawn(async move {
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
                tracing::error!("Run {} failed: {}", run_id_clone, e);
            }
        });

        Ok((run_id, receiver))
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
        let mut content_buffer = String::new();
        let mut total_tokens = 0u32;

        // Stream the response
        let stream = orchestrator.chat(messages).await?;
        futures::pin_mut!(stream);

        while let Some(event) = stream.next().await {
            // Collect content for session storage
            if let NormalizedEvent::MessageDelta { ref content, .. } = event.event {
                content_buffer.push_str(content);
            }

            // Collect usage
            if let NormalizedEvent::Usage { total_tokens: tokens, .. } = event.event {
                total_tokens = tokens;
            }

            // Forward event
            let _ = sender.send(event);
        }

        // Update session with assistant response
        {
            let mut sessions = sessions.write();
            if let Some(session) = sessions.get_mut(&session_id) {
                session.add_message(Message::assistant(&content_buffer));
                session.add_tokens(total_tokens);
            }
        }

        // Complete the run
        {
            let mut runs = active_runs.write();
            if let Some(state) = runs.get_mut(&run_id) {
                state.run.complete(&content_buffer);
                state.run.add_tokens(total_tokens, 0.0); // Cost calculation would go here
            }
        }

        Ok(())
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
