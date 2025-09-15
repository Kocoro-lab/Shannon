use std::collections::{HashMap, VecDeque};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

use anyhow::{anyhow, Result};
use tokio::time::timeout;

use crate::config::{Config, EnforcementConfig};
use crate::metrics::{ENFORCEMENT_ALLOWED, ENFORCEMENT_DROPS};

use redis::aio::ConnectionManager as RedisConnectionManager;
use redis::Script;

#[derive(Clone)]
pub struct RequestEnforcer {
    cfg: EnforcementConfig,
    // Simple per-key token bucket for rate limiting
    buckets: Arc<Mutex<HashMap<String, TokenBucket>>>,
    // Simple per-key rolling window circuit breaker
    breakers: Arc<Mutex<HashMap<String, RollingWindow>>>,
    // Optional distributed limiter
    redis: Option<RedisLimiter>,
}

impl RequestEnforcer {
    pub fn new(cfg: EnforcementConfig) -> Self {
        let redis = if let Some(url) = cfg.rate_redis_url.clone() {
            RedisLimiter::new(url, cfg.rate_redis_prefix.clone(), cfg.rate_redis_ttl_secs).ok()
        } else {
            None
        };
        Self {
            cfg,
            buckets: Arc::new(Mutex::new(HashMap::new())),
            breakers: Arc::new(Mutex::new(HashMap::new())),
            redis,
        }
    }

    pub fn from_global() -> Result<Self> {
        let cfg = Config::global()?.enforcement;
        Ok(Self::new(cfg))
    }

    async fn rate_check(&self, key: &str) -> Result<()> {
        if let Some(rl) = &self.redis {
            let cap = self.cfg.rate_limit_per_key_rps as f64;
            if rl.try_take(key, cap, cap).await? {
                return Ok(());
            }
            return Err(anyhow!("rate_limit_exceeded"));
        }
        let mut guard = self.buckets.lock().unwrap();
        let bucket = guard
            .entry(key.to_string())
            .or_insert_with(|| TokenBucket::new(self.cfg.rate_limit_per_key_rps as f64));
        if bucket.try_take(1.0) {
            Ok(())
        } else {
            Err(anyhow!("rate_limit_exceeded"))
        }
    }

    fn cb_allow(&self, key: &str) -> bool {
        let mut guard = self.breakers.lock().unwrap();
        let win = guard.entry(key.to_string()).or_insert_with(|| {
            RollingWindow::new(self.cfg.circuit_breaker_rolling_window_secs as usize)
        });
        if win.total < self.cfg.circuit_breaker_min_requests as usize {
            return true; // not enough data
        }
        win.error_rate() < self.cfg.circuit_breaker_error_threshold
    }

    fn cb_record(&self, key: &str, ok: bool) {
        let mut guard = self.breakers.lock().unwrap();
        let win = guard.entry(key.to_string()).or_insert_with(|| {
            RollingWindow::new(self.cfg.circuit_breaker_rolling_window_secs as usize)
        });
        win.push(ok);
    }

    pub async fn enforce<F, Fut, T>(&self, key: &str, est_tokens: usize, f: F) -> Result<T>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = Result<T>>,
    {
        // token ceiling (estimation only)
        if est_tokens > self.cfg.per_request_max_tokens {
            if let Some(c) = ENFORCEMENT_DROPS.get() {
                c.with_label_values(&["token_limit"]).inc();
            }
            return Err(anyhow!("token_limit_exceeded"));
        }

        // rate limit
        if self.rate_check(key).await.is_err() {
            if let Some(c) = ENFORCEMENT_DROPS.get() {
                c.with_label_values(&["rate_limit"]).inc();
            }
            return Err(anyhow!("rate_limit_exceeded"));
        }

        // circuit breaker
        if !self.cb_allow(key) {
            if let Some(c) = ENFORCEMENT_DROPS.get() {
                c.with_label_values(&["circuit_open"]).inc();
            }
            return Err(anyhow!("circuit_breaker_open"));
        }

        // timeout wrapper
        let res = timeout(Duration::from_secs(self.cfg.per_request_timeout_secs), f()).await;
        match res {
            Err(_) => {
                self.cb_record(key, false);
                if let Some(c) = ENFORCEMENT_DROPS.get() {
                    c.with_label_values(&["timeout"]).inc();
                }
                Err(anyhow!("request_timeout"))
            }
            Ok(Err(e)) => {
                self.cb_record(key, false);
                if let Some(c) = ENFORCEMENT_DROPS.get() {
                    c.with_label_values(&["downstream_error"]).inc();
                }
                Err(e)
            }
            Ok(Ok(v)) => {
                self.cb_record(key, true);
                if let Some(c) = ENFORCEMENT_ALLOWED.get() {
                    c.with_label_values(&["success"]).inc();
                }
                Ok(v)
            }
        }
    }
}

// --- helpers ---

#[derive(Clone)]
struct RedisLimiter {
    client: redis::Client,
    script: Script,
    prefix: String,
    ttl_ms: usize,
}

impl RedisLimiter {
    fn new(url: String, prefix: String, ttl_secs: u64) -> Result<Self> {
        let client = redis::Client::open(url)?;
        let script = Script::new(
            r#"
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local rate = tonumber(ARGV[2])
local now_ms = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])
local ttl_ms = tonumber(ARGV[5])

local data = redis.call('HMGET', key, 'tokens', 'ts')
local tokens = tonumber(data[1])
local ts = tonumber(data[2])
if tokens == nil then tokens = capacity end
if ts == nil then ts = now_ms end
local delta = now_ms - ts
if delta < 0 then delta = 0 end
local refill = (delta / 1000.0) * rate
tokens = math.min(capacity, tokens + refill)
local allowed = 0
if tokens >= requested then
  tokens = tokens - requested
  allowed = 1
end
redis.call('HMSET', key, 'tokens', tokens, 'ts', now_ms)
redis.call('PEXPIRE', key, ttl_ms)
return allowed
        "#,
        );
        Ok(Self {
            client,
            script,
            prefix,
            ttl_ms: (ttl_secs * 1000) as usize,
        })
    }

    async fn try_take(&self, key: &str, capacity: f64, rate_per_sec: f64) -> Result<bool> {
        let k = format!("{}{}", self.prefix, key);
        let now_ms = chrono::Utc::now().timestamp_millis();
        let mut manager = RedisConnectionManager::new(self.client.clone()).await?;
        let res: i32 = self
            .script
            .key(k)
            .arg(capacity)
            .arg(rate_per_sec)
            .arg(now_ms)
            .arg(1)
            .arg(self.ttl_ms as i64)
            .invoke_async(&mut manager)
            .await?;
        Ok(res == 1)
    }
}

#[derive(Clone, Debug)]
struct TokenBucket {
    capacity: f64,
    tokens: f64,
    refill_rate_per_sec: f64,
    last_refill: Instant,
}

impl TokenBucket {
    fn new(rps: f64) -> Self {
        Self {
            capacity: rps.max(1.0),
            tokens: rps.max(1.0),
            refill_rate_per_sec: rps.max(1.0),
            last_refill: Instant::now(),
        }
    }
    fn try_take(&mut self, amount: f64) -> bool {
        // refill
        let elapsed = self.last_refill.elapsed().as_secs_f64();
        let add = elapsed * self.refill_rate_per_sec;
        self.tokens = (self.tokens + add).min(self.capacity);
        self.last_refill = Instant::now();
        if self.tokens >= amount {
            self.tokens -= amount;
            true
        } else {
            false
        }
    }
}

#[derive(Clone, Debug)]
struct RollingWindow {
    window_secs: usize,
    events: VecDeque<(Instant, bool)>,
    total: usize,
    errors: usize,
}

impl RollingWindow {
    fn new(window_secs: usize) -> Self {
        Self {
            window_secs,
            events: VecDeque::new(),
            total: 0,
            errors: 0,
        }
    }
    fn prune(&mut self) {
        let cutoff = Instant::now() - Duration::from_secs(self.window_secs as u64);
        while let Some((t, ok)) = self.events.front().cloned() {
            if t < cutoff {
                self.events.pop_front();
                self.total -= 1;
                if !ok {
                    self.errors -= 1;
                }
            } else {
                break;
            }
        }
    }
    fn push(&mut self, ok: bool) {
        self.prune();
        self.events.push_back((Instant::now(), ok));
        self.total += 1;
        if !ok {
            self.errors += 1;
        }
    }
    fn error_rate(&mut self) -> f64 {
        self.prune();
        if self.total == 0 {
            0.0
        } else {
            self.errors as f64 / self.total as f64
        }
    }
}
