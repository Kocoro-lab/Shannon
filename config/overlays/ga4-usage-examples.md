# GA4 Usage Examples

This file contains example payloads for using GA4 analytics tools in Shannon.

**Note**: Replace placeholder values with your actual GA4 property ID and credentials.

---

## Prerequisites: Service Account Setup

Shannon uses a Google Cloud service account to authenticate with the GA4 Data API. Follow these steps to create one:

### 1. Create a Google Cloud Project (if needed)

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project or select an existing one
3. Note your project ID

### 2. Enable the GA4 Data API

1. In Cloud Console, go to **APIs & Services > Library**
2. Search for "Google Analytics Data API"
3. Click **Enable**

### 3. Create a Service Account

1. Go to **IAM & Admin > Service Accounts**
2. Click **Create Service Account**
3. Name it (e.g., `shannon-ga4-reader`)
4. Click **Create and Continue**
5. Skip the optional role assignment (GA4 permissions are granted separately)
6. Click **Done**

### 4. Generate a JSON Key

1. Click on your new service account
2. Go to **Keys** tab
3. Click **Add Key > Create new key**
4. Select **JSON** format
5. Save the downloaded file as `ga4-service-account-key.json`
6. Place it in `config/overlays/` (this path is gitignored for security)

### 5. Grant GA4 Property Access

1. Go to [Google Analytics](https://analytics.google.com/)
2. Select your GA4 property
3. Go to **Admin > Property Access Management**
4. Click **+** to add users
5. Enter your service account email (e.g., `shannon-ga4-reader@your-project.iam.gserviceaccount.com`)
6. Assign **Viewer** role (minimum required)
7. Click **Add**

### 6. Find Your GA4 Property ID

1. In Google Analytics, go to **Admin > Property Settings**
2. Copy the **Property ID** (numeric, e.g., `123456789`)

---

## Configuration

Create a config overlay with your GA4 settings:

```yaml
# config/overlays/shannon.vendor.yaml
ga4:
  property_id: "YOUR_GA4_PROPERTY_ID"
  credentials_path: "/app/config/overlays/ga4-service-account-key.json"
```

**Important**: The `ga4-service-account-key.json` file is gitignored to prevent accidental credential commits. Never commit service account keys to version control.

---

## Alternative: OAuth Access Token Authentication

For applications where users authenticate with their own Google accounts (e.g., multi-tenant SaaS), Shannon supports OAuth access tokens as an alternative to service account authentication.

### How It Works

```
┌──────────────┐    refresh_token    ┌─────────────────┐
│   Frontend   │ ─────────────────→  │  Google OAuth   │
│              │ ←───────────────── │                 │
└──────────────┘    access_token     └─────────────────┘
       │
       │  access_token + property_id (per request)
       ▼
┌──────────────┐
│   Shannon    │ ──→ GA4 API (using user's access token)
└──────────────┘
```

### Frontend Responsibilities

1. **Implement Google OAuth flow** to obtain refresh token (one-time user consent)
2. **Store refresh token securely** on your backend
3. **Exchange refresh token for access token** before each Shannon request
4. **Pass access token** in the task context (access tokens expire in ~1 hour)

### API Usage

Pass OAuth credentials in the task context instead of relying on server-side service account:

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "user-ga4-query",
  "query": "Show me my website traffic for the last 7 days",
  "context": {
    "role": "ga4_analytics",
    "ga4_access_token": "ya29.a0ARrdaM...",
    "ga4_property_id": "123456789"
  }
}'
```

### Key Differences from Service Account Mode

| Aspect | Service Account | OAuth Access Token |
|--------|-----------------|-------------------|
| **Setup** | One-time server config | Per-user OAuth flow |
| **Credentials** | JSON key file on server | Access token per request |
| **Use Case** | Single GA4 property | Multi-tenant / user-owned data |
| **Token Lifetime** | Long-lived | ~1 hour (frontend refreshes) |
| **Config Required** | `config/overlays/shannon.vendor.yaml` | None (credentials in request) |

### Security Considerations

- **Never send refresh tokens** to Shannon - only short-lived access tokens
- Access tokens expire in ~1 hour, limiting exposure if intercepted
- Shannon does not store OAuth tokens - they're used per-request only
- Use HTTPS in production to protect tokens in transit

### Required OAuth Scopes

When implementing the OAuth flow, request these scopes:

```
https://www.googleapis.com/auth/analytics.readonly
```

---

## Basic GA4 Query Examples

### 1. Daily Traffic Overview (Last 30 Days)

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-traffic-overview",
  "query": "Show me daily sessions, users, and page views for the last 30 days",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

---

### 2. Organic Traffic (Exclude Paid Traffic)

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-organic-traffic",
  "query": "Show me daily sessions for September 2025. EXCLUDE paid traffic: exclude sessions where sessionSourceMedium contains \"cpc\" or \"paid\". Only show organic and direct traffic with daily breakdown.",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

**Key Points**:
- ✅ Uses server-side `dimension_filter` with NOT + OR pattern
- ✅ Only includes `date` dimension (not `sessionSourceMedium`)
- ✅ No REGEXP - uses CONTAINS with NOT wrapper

---

### 3. Geographic Traffic Breakdown

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-geo-traffic",
  "query": "Show me sessions by country for the last 7 days, ordered by sessions descending",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

---

### 4. Device Category Breakdown

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-device-breakdown",
  "query": "Analyze last 30 days sessions by device category (desktop, mobile, tablet). Show engagement rate and bounce rate.",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

---

### 5. Top Landing Pages

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-landing-pages",
  "query": "Show me top 20 landing pages by sessions in the last 30 days, include bounce rate",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

---

### 6. Traffic Source Analysis

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-traffic-sources",
  "query": "Analyze traffic sources (organic, direct, referral, social) for last 30 days. Show sessions, engagement rate, and average session duration.",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

---

### 7. Specific Country Traffic (Server-Side Filter)

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-us-traffic",
  "query": "Show me daily sessions from United States only for last 30 days",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

Expected tool call:
```python
{
  "dimensions": ["date"],
  "metrics": ["sessions"],
  "dimension_filter": {
    "field": "country",
    "value": "United States",
    "match_type": "EXACT"
  }
}
```

---

### 8. Real-Time Traffic

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-realtime",
  "query": "Show me real-time active users right now, broken down by country",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

**Uses**: `ga4_run_realtime_report` (last 30 minutes)

---

### 9. Campaign Performance

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-campaign-perf",
  "query": "Show me performance of all campaigns in August 2025. Include sessions, key events (conversions), and engagement rate.",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-01"
    }
  }
}'
```

---

### 10. Multi-Condition Filter (Exclude Multiple Values)

```bash
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Content-Type: application/json" \
  -d '{
  "session_id": "ga4-multi-exclude",
  "query": "Show daily sessions for September 2025. EXCLUDE: (1) paid traffic (cpc, paid, ppc), (2) social traffic (facebook, twitter, linkedin). Only organic search and direct.",
  "context": {
    "role": "ga4_analytics",
    "prompt_params": {
      "profile_id": "YOUR_PROFILE_ID",
      "aid": "YOUR_ACCOUNT_ID",
      "current_date": "2025-09-30"
    }
  }
}'
```

Expected tool call with complex filter:
```python
{
  "dimensions": ["date"],
  "metrics": ["sessions"],
  "dimension_filter": {
    "not": {
      "or": [
        {"field": "sessionSourceMedium", "value": "cpc", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "paid", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "ppc", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "facebook", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "twitter", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "linkedin", "match_type": "CONTAINS"}
      ]
    }
  }
}
```

---

## Filter Pattern Reference

### ✅ Correct Patterns (Server-Side)

**Single exclusion:**
```python
{
  "dimension_filter": {
    "not": {
      "field": "sessionSourceMedium",
      "value": "paid",
      "match_type": "CONTAINS"
    }
  }
}
```

**Multiple exclusions (OR):**
```python
{
  "dimension_filter": {
    "not": {
      "or": [
        {"field": "sessionSourceMedium", "value": "cpc", "match_type": "CONTAINS"},
        {"field": "sessionSourceMedium", "value": "paid", "match_type": "CONTAINS"}
      ]
    }
  }
}
```

**Include specific values (IN):**
```python
{
  "dimension_filter": {
    "field": "country",
    "values": ["United States", "Canada", "United Kingdom"],
    "operator": "IN"
  }
}
```

**Multiple conditions (AND):**
```python
{
  "dimension_filter": {
    "and": [
      {"field": "country", "value": "United States", "match_type": "EXACT"},
      {"field": "deviceCategory", "value": "mobile", "match_type": "EXACT"}
    ]
  }
}
```

---

### ❌ Incorrect Patterns (Will Fail)

**Don't use REGEXP:**
```python
# ❌ WRONG
{
  "dimension_filter": {
    "field": "sessionSourceMedium",
    "value": "^(organic|direct)",
    "match_type": "REGEXP"  # NOT SUPPORTED!
  }
}
```

**Don't include filtered dimension:**
```python
# ❌ WRONG - includes sessionSourceMedium in dimensions
{
  "dimensions": ["date", "sessionSourceMedium"],  # Wrong!
  "dimension_filter": {
    "not": {"field": "sessionSourceMedium", ...}
  }
}

# ✅ CORRECT - only date dimension
{
  "dimensions": ["date"],  # Correct!
  "dimension_filter": {
    "not": {"field": "sessionSourceMedium", ...}
  }
}
```

---

## Common GA4 Dimensions

- `date` - Date (YYYYMMDD format)
- `country` - Country name
- `city` - City name
- `deviceCategory` - desktop, mobile, tablet
- `sessionSourceMedium` - Combined source/medium (e.g., "google / organic")
- `sessionSource` - Traffic source
- `sessionMedium` - Traffic medium
- `campaignName` - Campaign name
- `landingPage` - Landing page path
- `hostName` - Website hostname
- `pagePath` - Page URL path
- `browser` - Browser name
- `operatingSystem` - OS name

---

## Common GA4 Metrics

- `activeUsers` - Number of distinct users
- `sessions` - Number of sessions
- `screenPageViews` - Page/screen view count
- `engagementRate` - Percentage of engaged sessions
- `bounceRate` - Percentage of non-engaged sessions
- `averageSessionDuration` - Average session length (seconds)
- `keyEvents` - Total key events (formerly conversions, deprecated March 2024)
- `totalRevenue` - Total revenue (requires ecommerce)
- `eventCount` - Total events

---

## Tips for Effective Queries

1. **Be specific about date ranges**: "last 30 days", "September 2025", "last week"
2. **Use natural language for filters**: "exclude paid traffic", "only mobile users", "US traffic only"
3. **Request specific metrics**: "sessions, bounce rate, engagement rate"
4. **Ask for insights**: "analyze trends", "identify patterns", "compare periods"
5. **Specify sorting**: "top 10 pages", "ordered by sessions"

---

## Checking Results

```bash
# Submit task and get task_id
TASK_ID="task-00000000-0000-0000-0000-000000000002-XXXXXXXXXX"

# Check task status
curl -sS "http://localhost:8080/api/v1/tasks/$TASK_ID" | jq '.'

# Get result only
curl -sS "http://localhost:8080/api/v1/tasks/$TASK_ID" | jq -r '.result'
```

---

## Troubleshooting

### Issue: "GA4 tools are unavailable"
**Solution**: Check `config/overlays/shannon.vendor.yaml` has correct GA4 configuration

### Issue: "Invalid filter: match_type must be one of..."
**Solution**: Don't use REGEXP. Use CONTAINS with NOT wrapper for exclusions

### Issue: Filter not working / seeing excluded traffic
**Solution**: Make sure you're NOT including the filtered dimension in the `dimensions` list

### Issue: No data returned
**Solution**:
- Check date range (future dates return no data)
- Verify GA4 property ID is correct
- Ensure service account has Analytics Viewer permissions

---

## Related Documentation

- **Shannon Vendor Adapters**: `docs/vendor-adapters.md`
- **GA4 Data API**: https://developers.google.com/analytics/devguides/reporting/data/v1
- **Filter Construction**: See system prompt in `python/llm-service/llm_service/roles/ga4/analytics_agent.py`

---

**Last Updated**: 2025-11-04
**Shannon Version**: v1.0
