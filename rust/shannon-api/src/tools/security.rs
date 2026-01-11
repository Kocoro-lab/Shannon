//! Tool security and access control.
//!
//! This module provides allowlist/blocklist functionality
//! to control which tools can be used.

use std::collections::HashSet;
use std::sync::Arc;
use tokio::sync::RwLock;

/// Tool security policy.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum SecurityPolicy {
    /// Allow all tools (default).
    AllowAll,
    /// Allow only listed tools (allowlist).
    AllowList,
    /// Block only listed tools (blocklist).
    BlockList,
}

/// Tool security manager.
#[derive(Clone)]
pub struct ToolSecurity {
    /// Current security policy.
    policy: Arc<RwLock<SecurityPolicy>>,
    /// Allowlist of tool names.
    allowlist: Arc<RwLock<HashSet<String>>>,
    /// Blocklist of tool names.
    blocklist: Arc<RwLock<HashSet<String>>>,
}

impl ToolSecurity {
    /// Create a new tool security manager with default policy (allow all).
    #[must_use]
    pub fn new() -> Self {
        Self {
            policy: Arc::new(RwLock::new(SecurityPolicy::AllowAll)),
            allowlist: Arc::new(RwLock::new(HashSet::new())),
            blocklist: Arc::new(RwLock::new(HashSet::new())),
        }
    }

    /// Set the security policy.
    pub async fn set_policy(&self, policy: SecurityPolicy) {
        let mut p = self.policy.write().await;
        *p = policy;
    }

    /// Get the current security policy.
    pub async fn get_policy(&self) -> SecurityPolicy {
        *self.policy.read().await
    }

    /// Add a tool to the allowlist.
    pub async fn allow_tool(&self, tool_name: impl Into<String>) {
        let mut allowlist = self.allowlist.write().await;
        allowlist.insert(tool_name.into());
    }

    /// Remove a tool from the allowlist.
    pub async fn disallow_tool(&self, tool_name: &str) -> bool {
        let mut allowlist = self.allowlist.write().await;
        allowlist.remove(tool_name)
    }

    /// Add a tool to the blocklist.
    pub async fn block_tool(&self, tool_name: impl Into<String>) {
        let mut blocklist = self.blocklist.write().await;
        blocklist.insert(tool_name.into());
    }

    /// Remove a tool from the blocklist.
    pub async fn unblock_tool(&self, tool_name: &str) -> bool {
        let mut blocklist = self.blocklist.write().await;
        blocklist.remove(tool_name)
    }

    /// Check if a tool is allowed to be used.
    ///
    /// # Returns
    ///
    /// - `Ok(())` if the tool is allowed
    /// - `Err(String)` with reason if the tool is not allowed
    pub async fn check_allowed(&self, tool_name: &str) -> Result<(), String> {
        let policy = self.policy.read().await;

        match *policy {
            SecurityPolicy::AllowAll => {
                // Check blocklist even in allow-all mode
                let blocklist = self.blocklist.read().await;
                if blocklist.contains(tool_name) {
                    Err(format!("Tool '{}' is blocked", tool_name))
                } else {
                    Ok(())
                }
            }
            SecurityPolicy::AllowList => {
                let allowlist = self.allowlist.read().await;
                if allowlist.contains(tool_name) {
                    Ok(())
                } else {
                    Err(format!("Tool '{}' is not in allowlist", tool_name))
                }
            }
            SecurityPolicy::BlockList => {
                let blocklist = self.blocklist.read().await;
                if blocklist.contains(tool_name) {
                    Err(format!("Tool '{}' is blocked", tool_name))
                } else {
                    Ok(())
                }
            }
        }
    }

    /// Get all allowed tool names.
    pub async fn get_allowlist(&self) -> Vec<String> {
        let allowlist = self.allowlist.read().await;
        allowlist.iter().cloned().collect()
    }

    /// Get all blocked tool names.
    pub async fn get_blocklist(&self) -> Vec<String> {
        let blocklist = self.blocklist.read().await;
        blocklist.iter().cloned().collect()
    }

    /// Clear the allowlist.
    pub async fn clear_allowlist(&self) {
        let mut allowlist = self.allowlist.write().await;
        allowlist.clear();
    }

    /// Clear the blocklist.
    pub async fn clear_blocklist(&self) {
        let mut blocklist = self.blocklist.write().await;
        blocklist.clear();
    }
}

impl Default for ToolSecurity {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn test_allow_all_policy() {
        let security = ToolSecurity::new();
        assert!(security.check_allowed("any_tool").await.is_ok());
    }

    #[tokio::test]
    async fn test_allowlist_policy() {
        let security = ToolSecurity::new();
        security.set_policy(SecurityPolicy::AllowList).await;
        security.allow_tool("allowed_tool").await;

        assert!(security.check_allowed("allowed_tool").await.is_ok());
        assert!(security.check_allowed("other_tool").await.is_err());
    }

    #[tokio::test]
    async fn test_blocklist_policy() {
        let security = ToolSecurity::new();
        security.set_policy(SecurityPolicy::BlockList).await;
        security.block_tool("blocked_tool").await;

        assert!(security.check_allowed("blocked_tool").await.is_err());
        assert!(security.check_allowed("other_tool").await.is_ok());
    }
}
