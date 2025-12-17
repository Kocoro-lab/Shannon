# Web Search Configuration

The Shannon platform supports multiple web search providers to deliver real-time information for AI agents. Configure your preferred provider through environment variables.

## Supported Providers

### 1. **Google Custom Search** (Most Widely Used)
Industry-standard search with comprehensive coverage and rich results.
```bash
export WEB_SEARCH_PROVIDER=google
export GOOGLE_SEARCH_API_KEY=your_api_key_here
export GOOGLE_SEARCH_ENGINE_ID=your_search_engine_id_here
```
- Get API key at: https://console.cloud.google.com/apis/credentials
- Create search engine at: https://programmablesearchengine.google.com/
- Free tier: 100 queries/day, then $5 per 1000 queries
- Rate limit: 100 queries per 100 seconds

### 2. **Serper** (Best Value for Developers)
Fast, affordable Google search results API with simple pricing.
```bash
export WEB_SEARCH_PROVIDER=serper
export SERPER_API_KEY=your_api_key_here
```
- Get API key at: https://serper.dev
- Free tier: 2500 queries on signup
- Pricing: Starting at $50 for 50k queries ($1.00/1k)
- Rate limit: 300 queries/second

### 3. **SerpAPI** (Robust Google Search API)
Reliable API for Google Search, Maps, News, and more. Handles proxies and captchas effectively.
```bash
export WEB_SEARCH_PROVIDER=serpapi
export SERPAPI_API_KEY=your_api_key_here
```
- Get API key at: https://serpapi.com
- Free tier: 100 searches/month
- Pricing: Starting at $50 for 5k searches
- Rate Limit: Flexible based on plan

### 4. **Bing Search API** (Enterprise Choice)
Microsoft's search API with Azure integration.
```bash
export WEB_SEARCH_PROVIDER=bing
export BING_API_KEY=your_api_key_here
```
- Get API key at: https://azure.microsoft.com/en-us/services/cognitive-services/bing-web-search-api/
- Free tier: 1000 queries/month
- Pricing: $3 per 1000 queries for S1 tier
- Note: Bing Search APIs retiring August 11, 2025 - consider migration plans

- Note: Bing Search APIs retiring August 11, 2025 - consider migration plans

### 5. **Exa** (AI-Optimized Semantic Search)
Neural search with semantic understanding, optimized for AI applications.
```bash
export WEB_SEARCH_PROVIDER=exa
export EXA_API_KEY=your_api_key_here
```
- Get API key at: https://exa.ai
- Features: Semantic search, autoprompting, highlights extraction
- Free tier: 1000 queries/month
- Pricing: $0.001 per search

- Pricing: $0.001 per search

### 6. **Firecrawl** (Search + Content Extraction)
Web search with integrated scraping and markdown extraction.
```bash
export WEB_SEARCH_PROVIDER=firecrawl
export FIRECRAWL_API_KEY=your_api_key_here
```
- Get API key at: https://firecrawl.dev
- Features: Full content extraction, markdown formatting
- Free tier: Limited alpha access
- Pricing: Variable based on scraping depth

## Docker Compose Configuration

Add to your `deploy/compose/.env` file:
```env
# Web Search Configuration (choose one provider)
WEB_SEARCH_PROVIDER=google

# Google Custom Search (recommended)
GOOGLE_SEARCH_API_KEY=your_google_api_key_here
GOOGLE_SEARCH_ENGINE_ID=your_search_engine_id_here

# Or Serper (simple and affordable)
# WEB_SEARCH_PROVIDER=serper
# SERPER_API_KEY=your_serper_api_key_here

# Or SerpAPI (robust scraping)
# WEB_SEARCH_PROVIDER=serpapi
# SERPAPI_API_KEY=your_serpapi_api_key_here

# Or Bing (enterprise)
# WEB_SEARCH_PROVIDER=bing
# BING_API_KEY=your_bing_api_key_here

# Or Exa (semantic AI search)
# WEB_SEARCH_PROVIDER=exa
# EXA_API_KEY=your_exa_api_key_here

# Or Firecrawl (with content extraction)
# WEB_SEARCH_PROVIDER=firecrawl
# FIRECRAWL_API_KEY=your_firecrawl_api_key_here
```

The environment variables are already configured in `deploy/compose/docker-compose.yml`:
```yaml
llm-service:
  environment:
    - WEB_SEARCH_PROVIDER=${WEB_SEARCH_PROVIDER:-google}
    - GOOGLE_SEARCH_API_KEY=${GOOGLE_SEARCH_API_KEY}
    - GOOGLE_SEARCH_ENGINE_ID=${GOOGLE_SEARCH_ENGINE_ID}
    - SERPER_API_KEY=${SERPER_API_KEY}
    - BING_API_KEY=${BING_API_KEY}
    - BING_API_KEY=${BING_API_KEY}
    - SERPAPI_API_KEY=${SERPAPI_API_KEY}
    - EXA_API_KEY=${EXA_API_KEY}
    - FIRECRAWL_API_KEY=${FIRECRAWL_API_KEY}
```

## Fallback Behavior

If the configured provider is not available (missing API key or configuration), the system automatically tries other providers in this priority order:
1. Google Custom Search
2. Serper
3. SerpAPI
4. Bing
5. Exa
6. Firecrawl

If no provider is configured, web search will be disabled but the system continues to function.

## Response Format

All providers return normalized results with these fields:
- `title`: Result title
- `snippet`: Short text preview
- `content`: Extended content (when available)
- `url`: Result URL
- `source`: Provider name
- Additional provider-specific fields (score, date, highlights, etc.)

## Testing Your Configuration

Test your web search configuration:
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "web_search",
    "parameters": {
      "query": "latest AI news",
      "max_results": 5
    }
  }'
```

## Provider Comparison

| Provider | Best For | Pricing (per 1k queries) | Rate Limit | Content Depth |
|----------|----------|---------------------------|------------|---------------|
| **Google** | General search, comprehensive results | $5 after free tier | 100/100s | Snippets + metadata |
| **Serper** | High-volume, cost-effective | $0.30-$1.00 | 300/s | Snippets + knowledge graph |
| **SerpAPI** | Scraping reliability & variety | $10 | Varies | Snippets + rich result data |
| **Bing** | Enterprise, Azure integration | $3 | Varies by tier | Snippets |
| **Exa** | AI agents, semantic search | $1 | Standard | Full text + highlights |
| **Firecrawl** | Content extraction | Variable | Limited | Full markdown content |

## Setting Up Google Custom Search

1. **Create a Custom Search Engine:**
   - Go to https://programmablesearchengine.google.com/
   - Click "Add" to create a new search engine
   - Configure to search the entire web
   - Note your Search Engine ID

2. **Get API Key:**
   - Go to https://console.cloud.google.com/
   - Create a new project or select existing
   - Enable "Custom Search API"
   - Create credentials (API Key)
   - Restrict key to Custom Search API

3. **Configure Shannon:**
   ```bash
   export GOOGLE_SEARCH_API_KEY=your_key
   export GOOGLE_SEARCH_ENGINE_ID=your_engine_id
   export WEB_SEARCH_PROVIDER=google
   ```

## Best Practices

1. **Start with Google or Serper** for broad compatibility and reliable results
2. **Use Exa** for AI-specific semantic search needs
3. **Configure multiple providers** for redundancy
4. **Monitor usage** to stay within free tiers or budget
5. **Cache results** when possible to reduce API calls

## Troubleshooting

If web search isn't working:
1. Check environment variables are set correctly
2. Verify API keys are valid and have sufficient quota
3. Check logs: `docker compose logs llm-service`
4. Test provider directly with curl to isolate issues
5. Ensure network connectivity from your deployment environment

For provider-specific issues, consult their respective documentation and status pages.
