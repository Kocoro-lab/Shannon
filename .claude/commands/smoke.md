Run E2E smoke tests to verify the system is working.

Steps:
1. Ensure dev environment is running (`make dev`)
2. Run `make smoke` to execute smoke tests
3. Check test output for failures
4. If failures, check service logs: `docker compose -f deploy/compose/docker-compose.yml logs -f`
