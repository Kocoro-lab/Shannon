Start the local development environment.

Steps:
1. Run `make dev` to start all services (Postgres, Redis, Temporal, Qdrant, etc.)
2. Wait for services to be healthy
3. Verify with `docker compose -f deploy/compose/docker-compose.yml ps`
4. Check logs if any service is unhealthy: `docker compose -f deploy/compose/docker-compose.yml logs <service>`
