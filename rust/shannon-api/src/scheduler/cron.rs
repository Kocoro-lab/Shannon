//! Cron expression parsing and evaluation.
//!
//! This module provides a simple cron parser for schedule management.
//! Supports standard cron format: `minute hour day month weekday`.

use anyhow::{Context, Result};
use chrono::{DateTime, Datelike, Timelike, Utc};

/// A parsed cron expression.
#[derive(Debug, Clone)]
pub struct CronExpression {
    /// Minute (0-59).
    minute: CronField,
    /// Hour (0-23).
    hour: CronField,
    /// Day of month (1-31).
    day: CronField,
    /// Month (1-12).
    month: CronField,
    /// Day of week (0-6, Sunday = 0).
    weekday: CronField,
}

/// A single field in a cron expression.
#[derive(Debug, Clone)]
enum CronField {
    /// Wildcard (*) - matches all values.
    Any,
    /// Specific value.
    Value(u32),
    /// List of values (e.g., 1,3,5).
    List(Vec<u32>),
    /// Range (e.g., 1-5).
    Range(u32, u32),
    /// Step (e.g., */5).
    Step(u32),
}

impl CronField {
    /// Check if the field matches the given value.
    fn matches(&self, value: u32) -> bool {
        match self {
            Self::Any => true,
            Self::Value(v) => *v == value,
            Self::List(values) => values.contains(&value),
            Self::Range(start, end) => value >= *start && value <= *end,
            Self::Step(step) => value % step == 0,
        }
    }
}

/// Cron expression parser.
pub struct CronParser;

impl CronParser {
    /// Parse a cron expression string.
    ///
    /// # Format
    ///
    /// Standard cron format: `minute hour day month weekday`
    ///
    /// # Examples
    ///
    /// - `0 0 * * *` - Daily at midnight
    /// - `*/5 * * * *` - Every 5 minutes
    /// - `0 9-17 * * 1-5` - Every hour 9am-5pm, Monday-Friday
    ///
    /// # Errors
    ///
    /// Returns an error if the expression is invalid.
    pub fn parse(expr: &str) -> Result<CronExpression> {
        let parts: Vec<&str> = expr.split_whitespace().collect();
        if parts.len() != 5 {
            anyhow::bail!("Cron expression must have 5 fields: {}", expr);
        }

        Ok(CronExpression {
            minute: Self::parse_field(parts[0], 0, 59).context("Invalid minute field")?,
            hour: Self::parse_field(parts[1], 0, 23).context("Invalid hour field")?,
            day: Self::parse_field(parts[2], 1, 31).context("Invalid day field")?,
            month: Self::parse_field(parts[3], 1, 12).context("Invalid month field")?,
            weekday: Self::parse_field(parts[4], 0, 6).context("Invalid weekday field")?,
        })
    }

    fn parse_field(field: &str, min: u32, max: u32) -> Result<CronField> {
        // Wildcard
        if field == "*" {
            return Ok(CronField::Any);
        }

        // Step (*/n)
        if let Some(step_str) = field.strip_prefix("*/") {
            let step: u32 = step_str.parse().context("Invalid step value")?;
            if step == 0 || step > max {
                anyhow::bail!("Step value must be 1-{}", max);
            }
            return Ok(CronField::Step(step));
        }

        // Range (n-m)
        if field.contains('-') {
            let range_parts: Vec<&str> = field.split('-').collect();
            if range_parts.len() != 2 {
                anyhow::bail!("Invalid range format: {}", field);
            }
            let start: u32 = range_parts[0].parse().context("Invalid range start")?;
            let end: u32 = range_parts[1].parse().context("Invalid range end")?;
            if start < min || start > max || end < min || end > max || start > end {
                anyhow::bail!("Range values must be {}-{} with start <= end", min, max);
            }
            return Ok(CronField::Range(start, end));
        }

        // List (n,m,...)
        if field.contains(',') {
            let values: Result<Vec<u32>> = field
                .split(',')
                .map(|v| {
                    let num: u32 = v.parse().context("Invalid list value")?;
                    if num < min || num > max {
                        anyhow::bail!("Value must be {}-{}", min, max);
                    }
                    Ok(num)
                })
                .collect();
            return Ok(CronField::List(values?));
        }

        // Single value
        let value: u32 = field.parse().context("Invalid numeric value")?;
        if value < min || value > max {
            anyhow::bail!("Value must be {}-{}", min, max);
        }
        Ok(CronField::Value(value))
    }
}

impl CronExpression {
    /// Check if the cron expression matches the given time.
    pub fn matches(&self, time: &DateTime<Utc>) -> bool {
        self.minute.matches(time.minute())
            && self.hour.matches(time.hour())
            && self.day.matches(time.day())
            && self.month.matches(time.month())
            && self.weekday.matches(time.weekday().num_days_from_sunday())
    }

    /// Calculate the next execution time after the given time.
    pub fn next_after(&self, after: &DateTime<Utc>) -> Option<DateTime<Utc>> {
        // Simple implementation: check next 365 days
        let mut current = *after + chrono::Duration::minutes(1);
        for _ in 0..(365 * 24 * 60) {
            if self.matches(&current) {
                return Some(current);
            }
            current += chrono::Duration::minutes(1);
        }
        None
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_parse_wildcard() {
        let expr = CronParser::parse("* * * * *").unwrap();
        let now = Utc::now();
        assert!(expr.matches(&now));
    }

    #[test]
    fn test_parse_daily_midnight() {
        let expr = CronParser::parse("0 0 * * *").unwrap();
        let midnight = Utc::now()
            .date_naive()
            .and_hms_opt(0, 0, 0)
            .unwrap()
            .and_utc();
        assert!(expr.matches(&midnight));
    }

    #[test]
    fn test_parse_invalid() {
        assert!(CronParser::parse("invalid").is_err());
        assert!(CronParser::parse("* * *").is_err());
        assert!(CronParser::parse("60 * * * *").is_err());
    }
}
