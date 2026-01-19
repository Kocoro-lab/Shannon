# Trading Agents Usage Guide

**Date**: 2025-01-18
**Version**: V2.1 (27 roles, 5 templates)

## Quick Reference

| Template | Use Case | Roles | Typical Latency | Est. Cost |
|----------|----------|-------|-----------------|-----------|
| `trading_analysis` | Intraday trade signals | 7 | ~15-30s | $0.05-0.10 |
| `event_catalyst` | Pre-event positioning | 4 | ~10-20s | $0.03-0.05 |
| `regime_detection` | Market regime classification | 4 | ~10-15s | $0.02-0.03 |
| `famous_investors` | Deep analysis reports | 8 | ~30-60s | $0.08-0.15 |
| `news_monitor` | Fast news sentiment | 4 | ~20-30s | **$0.003** |

---

## Template 1: trading_analysis

**Purpose**: Generate TradeIntent for a specific ticker with bull/bear debate.

### Request

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Generate trading recommendation for NVDA",
    "context": {
      "template": "trading_analysis",
      "enable_citations": false,
      "prompt_params": {
        "ticker": "NVDA",
        "current_date": "2025-01-18",
        "data_snapshot_json": "{\"id\":\"snap_123\",\"as_of\":\"2025-01-18T14:30:00Z\",\"price_data\":{\"current\":142.50,\"vwap\":141.85},\"technicals\":{\"rsi_14\":65.2,\"macd\":{\"value\":1.2,\"signal\":0.8}},\"options_summary\":{\"iv_rank\":45,\"put_call_ratio\":0.85},\"news_summary\":{\"sentiment_score\":0.6},\"fundamentals_snapshot\":{\"pe_ratio\":65.2,\"next_earnings\":\"2025-02-26\"},\"constraints\":{\"max_buy_qty\":100,\"max_sell_qty\":0,\"buying_power_usd\":25000}}"
      }
    }
  }'
```

### Required prompt_params

| Parameter | Type | Description |
|-----------|------|-------------|
| `ticker` | string | Stock symbol (e.g., "NVDA") |
| `current_date` | string | ISO date (e.g., "2025-01-18") |
| `data_snapshot_json` | string | JSON string with market data (see schema below) |

### data_snapshot_json Schema

```json
{
  "id": "snap_123",
  "as_of": "2025-01-18T14:30:00Z",
  "price_data": {
    "current": 142.50,
    "vwap": 141.85,
    "high": 143.20,
    "low": 140.10,
    "volume": 15000000
  },
  "technicals": {
    "rsi_14": 65.2,
    "macd": {"value": 1.2, "signal": 0.8, "histogram": 0.4},
    "sma_20": 140.00,
    "sma_50": 135.00,
    "sma_200": 125.00,
    "atr_14": 3.50
  },
  "options_summary": {
    "iv_rank": 45,
    "iv_percentile": 52,
    "put_call_ratio": 0.85,
    "max_pain": 140.00,
    "notable_flow": [
      {"strike": 150, "expiry": "2025-02-21", "type": "call", "volume": 5000, "oi": 12000}
    ]
  },
  "news_summary": {
    "sentiment_score": 0.6,
    "headline_count": 15,
    "top_headlines": [
      {"title": "NVDA announces new AI chip", "sentiment": 0.8, "source": "Reuters"}
    ]
  },
  "fundamentals_snapshot": {
    "pe_ratio": 65.2,
    "market_cap_b": 3500,
    "next_earnings": "2025-02-26",
    "revenue_growth_yoy": 0.45
  },
  "constraints": {
    "max_buy_qty": 100,
    "max_sell_qty": 0,
    "buying_power_usd": 25000
  }
}
```

### Output: TradeIntent

```json
{
  "schema_version": "1.0.0",
  "as_of": "2025-01-18T14:30:00Z",
  "data_snapshot_id": "snap_123",
  "valid_until": "2025-01-18T15:30:00Z",
  "instrument_type": "equity",
  "ticker": "NVDA",
  "action": "OPEN",
  "legs": [],
  "suggested_size": 0.03,
  "confidence": 0.72,
  "stop_loss": 135.00,
  "take_profit": 155.00,
  "time_horizon": "swing",
  "reasoning": "Strong technical setup with bullish sentiment ahead of earnings",
  "risk_factors": ["earnings_in_5_days", "high_valuation"],
  "bull_case_summary": "Breakout above resistance with volume",
  "bear_case_summary": "Valuation stretched, earnings risk",
  "constraints_checked": true,
  "constraint_violations": []
}
```

---

## Template 2: event_catalyst

**Purpose**: Pre-event analysis for earnings, FDA, M&A positioning.

### Request

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Analyze pre-earnings catalyst for NVDA",
    "context": {
      "template": "event_catalyst",
      "prompt_params": {
        "ticker": "NVDA",
        "current_date": "2025-01-18",
        "data_snapshot_json": "{\"id\":\"snap_456\",\"as_of\":\"2025-01-18T14:30:00Z\",\"event\":{\"type\":\"earnings\",\"date\":\"2025-02-26\",\"days_until\":39},\"earnings_snapshot\":{\"earnings_date\":\"2025-02-26\",\"consensus_eps\":0.85,\"whisper_eps\":0.92,\"estimate_revisions_30d\":5},\"historical_earnings\":[{\"quarter\":\"2024Q3\",\"eps_surprise_pct\":12.5},{\"quarter\":\"2024Q2\",\"eps_surprise_pct\":8.3}],\"guidance_history\":[{\"quarter\":\"2024Q3\",\"pattern\":\"raises\"},{\"quarter\":\"2024Q2\",\"pattern\":\"maintains\"}],\"options_snapshot\":{\"iv_rank\":72,\"atm_iv\":55.2,\"put_call_ratio\":0.75,\"unusual_activity\":true},\"implied_move\":{\"pct\":8.5},\"historical_events\":[{\"date\":\"2024-11-21\",\"event_type\":\"earnings\",\"surprise_pct\":12.5,\"price_move_pct\":9.2},{\"date\":\"2024-08-28\",\"event_type\":\"earnings\",\"surprise_pct\":8.3,\"price_move_pct\":-6.1}]}"
      }
    }
  }'
```

### Output Schema

```json
{
  "event_type": "earnings",
  "event_date": "2025-02-26",
  "days_until_event": 39,
  "expected_move_pct": 7.5,
  "options_implied_move_pct": 8.5,
  "historical_beat_rate": 1.0,
  "positioning_recommendation": "straddle",
  "confidence": 0.68,
  "reasoning": "High IV but consistent beat history suggests upside surprise possible",
  "risk_factors": ["iv_elevated", "high_expectations"]
}
```

---

## Template 3: regime_detection

**Purpose**: Market-wide regime classification for position sizing.

### Request

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Detect current market regime",
    "context": {
      "template": "regime_detection",
      "prompt_params": {
        "current_date": "2025-01-18",
        "data_snapshot_json": "{\"id\":\"regime_001\",\"as_of\":\"2025-01-18T16:00:00Z\",\"fed_policy\":{\"current_rate\":4.5,\"dot_plot_terminal\":4.0,\"last_statement_tone\":\"dovish\"},\"yields\":{\"us2y\":4.2,\"us10y\":4.5,\"spread_2s10s\":0.3},\"dollar\":{\"dxy\":103.5,\"trend\":\"weakening\"},\"economic_indicators\":{\"pmi\":52.1,\"unemployment\":3.8,\"cpi_yoy\":3.2},\"sector_performance\":{\"tech\":{\"1m\":0.08},\"consumer_discretionary\":{\"1m\":0.05},\"utilities\":{\"1m\":-0.02},\"staples\":{\"1m\":-0.01}},\"relative_strength\":{\"tech_vs_spy\":1.12,\"utilities_vs_spy\":0.93},\"factor_performance\":{\"growth\":{\"1m\":0.06},\"value\":{\"1m\":0.02},\"momentum\":{\"1m\":0.07}},\"breadth\":{\"advance_decline_ratio\":1.4,\"pct_above_200dma\":0.62},\"vix\":{\"level\":15.2,\"percentile\":35},\"vix_term_structure\":{\"structure\":\"contango\",\"vix3m\":17.0},\"realized_vol\":{\"spy_20d\":12.5}}"
      }
    }
  }'
```

### Example Output (abbreviated)

The full response includes more fields (vote counts, confidence, risks/catalysts, profile match). Parse as flexible JSON.

```json
{
  "market_regime": "risk_on",
  "volatility_regime": "low",
  "sector_rotation": {
    "favored": ["tech", "consumer_discretionary"],
    "unfavored": ["utilities", "staples"]
  },
  "fed_policy_stance": "dovish",
  "position_sizing_multiplier": 1.0,
  "reasoning": "Low VIX, dovish Fed, tech leadership indicates risk-on environment",
  "valid_until": "2025-01-25T00:00:00Z"
}
```

---

## Template 4: famous_investors

**Purpose**: Deep analysis from multiple investment philosophy perspectives.

### Request

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Analyze AAPL from famous investor perspectives",
    "context": {
      "template": "famous_investors",
      "prompt_params": {
        "ticker": "AAPL",
        "current_date": "2025-01-18",
        "data_snapshot_json": "{\"id\":\"deep_001\",\"as_of\":\"2025-01-18T16:00:00Z\",\"price\":185.50,\"market_cap_b\":2850,\"fundamentals\":{\"pe_ratio\":28.5,\"peg_ratio\":2.1,\"roe\":147.5,\"debt_to_equity\":1.8,\"fcf_yield\":3.8,\"gross_margin\":45.0,\"operating_margin\":30.2},\"growth\":{\"revenue_growth_5y\":8.5,\"eps_growth_5y\":12.3,\"dividend_growth_5y\":6.2},\"moat_indicators\":{\"brand_value_rank\":1,\"ecosystem_lock_in\":\"high\",\"switching_cost\":\"high\"},\"management\":{\"ceo_tenure_years\":3,\"insider_ownership_pct\":0.02,\"buyback_yield\":2.5},\"valuation\":{\"dcf_fair_value\":175,\"graham_number\":95,\"owner_earnings_value\":190}}"
      }
    }
  }'
```

### Output Schema

```json
{
  "consensus": "bullish",
  "bull_investors": ["buffett", "lynch", "fisher"],
  "bear_investors": ["burry"],
  "neutral_investors": ["graham", "munger", "ackman"],
  "key_debate_points": [
    "Valuation: Graham sees overvalued vs DCF, Buffett sees moat premium justified",
    "Growth: Lynch likes services growth, Burry warns of China risk",
    "Moat: Consensus on strong ecosystem, debate on sustainability"
  ],
  "investment_thesis": "Quality compounder with strong moat but fully valued",
  "time_horizon": "long_term",
  "suitable_for": "long_term_value"
}
```

---

## Template 5: news_monitor

**Purpose**: Fast, token-efficient news sentiment monitoring. Designed for scheduled daily/hourly runs.

### Why news_monitor vs ResearchWorkflow?

| Metric | ResearchWorkflow | news_monitor |
|--------|-----------------|--------------|
| Execution | ReAct loops (iterative) | Single-pass parallel |
| Tokens | 300K+ | ~20K |
| Cost | $0.10-0.15 | **$0.003** |
| Latency | 2-5 minutes | ~25 seconds |
| Reliability | May fail/timeout | Consistent |

### Request

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Monitor TSLA news sentiment",
    "context": {
      "template": "news_monitor",
      "enable_citations": false,
      "prompt_params": {
        "ticker": "TSLA",
        "current_date": "2025-01-18"
      }
    }
  }'
```

### Required prompt_params

| Parameter | Type | Description |
|-----------|------|-------------|
| `ticker` | string | Stock symbol (e.g., "TSLA") |
| `current_date` | string | ISO date (e.g., "2025-01-18") |

### Output Schema

```json
{
  "ticker": "TSLA",
  "as_of": "2025-01-18",
  "overall_sentiment": {
    "score": -0.15,
    "label": "mixed",
    "confidence": 0.72
  },
  "news_summary": "Brief 1-2 sentence summary",
  "sentiment_breakdown": {
    "news_sentiment": -0.20,
    "analyst_sentiment": -0.10,
    "social_sentiment": -0.15
  },
  "key_headlines": [
    {"title": "...", "source": "...", "url": "...", "sentiment": "positive|negative|neutral"}
  ],
  "analyst_actions": [
    {"firm": "...", "action": "upgrade|downgrade|initiate", "price_target": "..."}
  ],
  "social_indicators": {
    "retail_sentiment": "bullish|bearish|neutral",
    "trending": true,
    "key_themes": ["theme1", "theme2"]
  },
  "catalysts": ["catalyst1", "catalyst2"],
  "risks": ["risk1", "risk2"]
}
```

### Scheduled Task Example

For daily automated news monitoring:

```bash
curl -X POST http://localhost:8080/api/v1/schedules \
  -H "Content-Type: application/json" \
  -d '{
    "name": "TSLA Daily News Monitor",
    "description": "Daily sentiment check for Tesla",
    "cron_expression": "0 9 * * *",
    "timezone": "America/New_York",
    "task_query": "Monitor TSLA news sentiment",
    "task_context": {
      "template": "news_monitor",
      "prompt_params.ticker": "TSLA",
      "prompt_params.current_date": "2025-01-18"
    },
    "max_budget_per_run_usd": 0.05,
    "timeout_seconds": 120
  }'
```

### Use Cases

| Scenario | Recommended |
|----------|-------------|
| Daily pre-market briefing | ✅ news_monitor |
| Intraday sentiment check | ✅ news_monitor |
| Deep research report | ❌ Use `famous_investors` |
| Trading signal generation | ❌ Use `trading_analysis` |

---

## Polling for Results

All templates are async. Poll for completion:

```bash
# Get task_id from POST response
TASK_ID="task-abc123"

# Poll until TASK_STATUS_COMPLETED
curl -sS "http://localhost:8080/api/v1/tasks/${TASK_ID}" | jq '{status, result}'
```

### Status Values

| Status | Meaning |
|--------|---------|
| `TASK_STATUS_QUEUED` | Queued |
| `TASK_STATUS_RUNNING` | Agents executing |
| `TASK_STATUS_COMPLETED` | Result ready in `result` (and parsed `response` when JSON) |
| `TASK_STATUS_FAILED` | Check `error` field |

---

## Data Size Limits

Shannon enforces context limits. The caller must compress:

| Data Type | Limit | Compression Strategy |
|-----------|-------|---------------------|
| Arrays | 100 elements | Top N most relevant |
| Strings | 10,000 chars | Summarize, truncate |
| OHLCV bars | 100 bars max | Resample or recent only |
| Options chain | ATM ±5 strikes | Nearest 2-3 expiries |
| News | 10-20 headlines | With sentiment scores |

---

## gRPC Alternative

```bash
# List templates
grpcurl -plaintext localhost:50052 \
  shannon.orchestrator.OrchestratorService/ListTemplates | \
  jq '.templates[] | select(.name | test("trading|event|regime|famous|news"))'

# Submit via gRPC
grpcurl -plaintext -d '{
  "metadata": {"userId": "trading-system", "sessionId": "session-001"},
  "query": "Generate trading recommendation for NVDA",
  "context": {
    "template": "trading_analysis",
    "prompt_params": {"ticker": "NVDA", "current_date": "2025-01-18", "data_snapshot_json": "{}"}
  }
}' localhost:50052 shannon.orchestrator.OrchestratorService/SubmitTask
```

---

## Error Handling

| Error | Cause | Solution |
|-------|-------|----------|
| `template not found` | Template name typo | Check exact name |
| `budget exceeded` | Token limit hit | Reduce data_snapshot size |
| `role not found` | Role not registered | Restart llm-service |
| `invalid JSON` | Malformed data_snapshot_json | Validate JSON encoding |

---

## Cost Estimates

| Template | Est. Tokens | Est. Cost (GPT-4o) |
|----------|-------------|-------------------|
| trading_analysis | ~40-50k | $0.05-0.10 |
| event_catalyst | ~20-30k | $0.03-0.05 |
| regime_detection | ~15-20k | $0.02-0.03 |
| famous_investors | ~50-70k | $0.08-0.15 |
