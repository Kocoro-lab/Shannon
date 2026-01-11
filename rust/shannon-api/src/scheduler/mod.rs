//! Schedule management for periodic task execution.
//!
//! This module provides cron-based scheduling capabilities for the Shannon API,
//! allowing users to schedule recurring tasks.

pub mod cron;
pub mod executor;

pub use cron::{CronExpression, CronParser};
pub use executor::{ScheduleExecutor, ScheduleRun};

use serde::{Deserialize, Serialize};

/// A scheduled task.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Schedule {
    /// Unique schedule ID.
    pub id: String,
    /// User who owns this schedule.
    pub user_id: String,
    /// Optional name/description.
    pub name: Option<String>,
    /// Cron expression (e.g., "0 0 * * *" for daily at midnight).
    pub cron: String,
    /// Task query to execute.
    pub query: String,
    /// Task strategy.
    pub strategy: String,
    /// Whether the schedule is enabled.
    pub enabled: bool,
    /// Creation timestamp.
    pub created_at: chrono::DateTime<chrono::Utc>,
    /// Last update timestamp.
    pub updated_at: chrono::DateTime<chrono::Utc>,
    /// Last execution timestamp.
    pub last_run_at: Option<chrono::DateTime<chrono::Utc>>,
    /// Next scheduled execution.
    pub next_run_at: Option<chrono::DateTime<chrono::Utc>>,
}
