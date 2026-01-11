//! Schedule execution engine.
//!
//! This module manages the execution of scheduled tasks,
//! tracking runs and handling recurring executions.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use tokio::sync::RwLock;

use super::{CronParser, Schedule};

/// A record of a schedule execution.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleRun {
    /// Unique run ID.
    pub id: String,
    /// Schedule ID this run belongs to.
    pub schedule_id: String,
    /// Workflow/task ID created for this run.
    pub task_id: String,
    /// Run status.
    pub status: String,
    /// Start time.
    pub started_at: DateTime<Utc>,
    /// Completion time.
    pub completed_at: Option<DateTime<Utc>>,
    /// Error message if failed.
    pub error: Option<String>,
}

/// Schedule execution engine.
///
/// This manages periodic execution of scheduled tasks.
#[derive(Clone)]
pub struct ScheduleExecutor {
    /// Active schedules.
    schedules: Arc<RwLock<HashMap<String, Schedule>>>,
    /// Run history.
    runs: Arc<RwLock<Vec<ScheduleRun>>>,
}

impl ScheduleExecutor {
    /// Create a new schedule executor.
    #[must_use]
    pub fn new() -> Self {
        Self {
            schedules: Arc::new(RwLock::new(HashMap::new())),
            runs: Arc::new(RwLock::new(Vec::new())),
        }
    }

    /// Add a schedule to the executor.
    pub async fn add_schedule(&self, schedule: Schedule) -> anyhow::Result<()> {
        // Validate cron expression
        CronParser::parse(&schedule.cron)?;

        let mut schedules = self.schedules.write().await;
        schedules.insert(schedule.id.clone(), schedule);
        Ok(())
    }

    /// Remove a schedule from the executor.
    pub async fn remove_schedule(&self, schedule_id: &str) -> bool {
        let mut schedules = self.schedules.write().await;
        schedules.remove(schedule_id).is_some()
    }

    /// Get a schedule by ID.
    pub async fn get_schedule(&self, schedule_id: &str) -> Option<Schedule> {
        let schedules = self.schedules.read().await;
        schedules.get(schedule_id).cloned()
    }

    /// List all schedules.
    pub async fn list_schedules(&self) -> Vec<Schedule> {
        let schedules = self.schedules.read().await;
        schedules.values().cloned().collect()
    }

    /// Record a schedule run.
    pub async fn record_run(&self, run: ScheduleRun) {
        let mut runs = self.runs.write().await;
        runs.push(run);
    }

    /// Get runs for a schedule.
    pub async fn get_runs(&self, schedule_id: &str, limit: usize) -> Vec<ScheduleRun> {
        let runs = self.runs.read().await;
        runs.iter()
            .filter(|r| r.schedule_id == schedule_id)
            .take(limit)
            .cloned()
            .collect()
    }

    /// Check for schedules that should execute now.
    pub async fn check_due_schedules(&self) -> Vec<Schedule> {
        let now = Utc::now();
        let schedules = self.schedules.read().await;

        schedules
            .values()
            .filter(|s| s.enabled && s.next_run_at.map(|next| next <= now).unwrap_or(false))
            .cloned()
            .collect()
    }
}

impl Default for ScheduleExecutor {
    fn default() -> Self {
        Self::new()
    }
}
