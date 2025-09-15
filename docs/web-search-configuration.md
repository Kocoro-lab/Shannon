# Web Search Configuration

The Shannon platform now supports multiple web search providers. Configure your preferred provider by setting environment variables.

## Supported Providers

### 1. **Tavily** (Recommended for AI Agents)
Fast, reliable search optimized for AI agents.
```bash
export WEB_SEARCH_PROVIDER=tavily
export TAVILY_API_KEY=your_api_key_here
```
Get API key at: https://tavily.com

### 2. **Exa** (formerly Metaphor)
Neural search with semantic understanding.
```bash
export WEB_SEARCH_PROVIDER=exa
export EXA_API_KEY=your_api_key_here
```
Get API key at: https://exa.ai

### 3. **Firecrawl**
Web scraping and search API.
```bash
export WEB_SEARCH_PROVIDER=firecrawl
export FIRECRAWL_API_KEY=your_api_key_here
```
Get API key at: https://firecrawl.dev

### 4. **Brave Search**
Privacy-focused search engine.
```bash
export WEB_SEARCH_PROVIDER=brave
export BRAVE_API_KEY=your_api_key_here
```
Get API key at: https://brave.com/search/api/

### 5. **SerpAPI**
Supports Google, Bing, DuckDuckGo and more.
```bash
export WEB_SEARCH_PROVIDER=serpapi
export SERPAPI_API_KEY=your_api_key_here
export SERPAPI_ENGINE=google  # Optional: google, bing, duckduckgo, etc.
```
Get API key at: https://serpapi.com

## Docker Compose Configuration

Add to your `deploy/compose/.env` file:
```env
# Web Search Configuration
WEB_SEARCH_PROVIDER=tavily
TAVILY_API_KEY=your_tavily_api_key_here

# Or use another provider:
# WEB_SEARCH_PROVIDER=exa
# EXA_API_KEY=your_exa_api_key_here
```

Then update `deploy/compose/compose.yml` to pass the environment variables:
```yaml
llm-service:
  environment:
    - WEB_SEARCH_PROVIDER=${WEB_SEARCH_PROVIDER:-tavily}
    - TAVILY_API_KEY=${TAVILY_API_KEY}
    - EXA_API_KEY=${EXA_API_KEY}
    - FIRECRAWL_API_KEY=${FIRECRAWL_API_KEY}
    - BRAVE_API_KEY=${BRAVE_API_KEY}
    - SERPAPI_API_KEY=${SERPAPI_API_KEY}
    - SERPAPI_ENGINE=${SERPAPI_ENGINE:-google}
```

## Fallback Behavior

If the configured provider is not available (missing API key), the system will automatically try other providers in this order:
1. Exa
2. Tavily
3. Firecrawl
4. Brave
5. SerpAPI

If no provider is configured, web search will be disabled but the system will continue to function.

## Testing

Test your configuration:
```bash
curl -X POST http://localhost:8000/tools/execute \
  -H "Content-Type: application/json" \
  -d '{
    "tool_name": "web_search",
    "parameters": {
      "query": "latest AI news",
      "max_results": 3
    }
  }'
```

## Cost Considerations

- **Tavily**: $0.0002 per search (free tier: 1000/month)
- **Exa**: $0.001 per search (free tier: 1000/month)
- **Firecrawl**: $0.0005 per search (free tier: 500/month)
- **Brave**: $0.0003 per search (free tier: 2000/month)
- **SerpAPI**: $0.005 per search (free tier: 100/month)

Choose based on your volume and quality requirements.