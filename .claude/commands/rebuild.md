Rebuild and restart a specific service after code changes.

Usage: /rebuild <service>

Where <service> is one of: orchestrator, gateway, llm-service, agent-core

Steps:
1. Build the service:
   - orchestrator: `docker build --no-cache -f go/orchestrator/Dockerfile -t shannon-orchestrator .`
   - gateway: `docker build --no-cache -f go/orchestrator/cmd/gateway/Dockerfile -t shannon-gateway .`
   - llm-service: `docker build --no-cache -f python/llm-service/Dockerfile -t shannon-llm-service .`
   - agent-core: `docker build --no-cache -f rust/agent-core/Dockerfile -t shannon-agent-core .`
2. Restart the service: `docker compose -f deploy/compose/docker-compose.yml up -d <service> --no-build`
3. Check logs: `docker compose -f deploy/compose/docker-compose.yml logs -f <service>`
