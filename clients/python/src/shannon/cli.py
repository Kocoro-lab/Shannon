"""Shannon CLI tool."""

import argparse
import os
import sys
import time

from shannon import ShannonClient, TaskStatusEnum, EventType, errors


def main():
    """Main CLI entry point."""
    parser = argparse.ArgumentParser(description="Shannon AI Platform CLI")
    parser.add_argument(
        "--base-url",
        default=os.getenv("SHANNON_BASE_URL", "http://localhost:8080"),
        help="Gateway base URL (default: http://localhost:8080)",
    )
    parser.add_argument(
        "--api-key",
        default=os.getenv("SHANNON_API_KEY", ""),
        help="API key for authentication",
    )
    parser.add_argument(
        "--bearer-token",
        default=os.getenv("SHANNON_BEARER_TOKEN", ""),
        help="Bearer token for authentication (alternative to API key)",
    )

    subparsers = parser.add_subparsers(dest="command", help="Command to execute")

    # Submit command
    submit_parser = subparsers.add_parser("submit", help="Submit a task")
    submit_parser.add_argument("query", help="Task query")
    submit_parser.add_argument("--session-id", help="Session ID")
    submit_parser.add_argument("--wait", action="store_true", help="Wait for completion")
    submit_parser.add_argument("--idempotency-key", help="Idempotency key for deduplicated submission")
    submit_parser.add_argument("--traceparent", help="W3C traceparent header for distributed tracing")
    submit_parser.add_argument("--model-tier", choices=["small", "medium", "large"], help="Model tier selection")
    submit_parser.add_argument("--model-override", help="Specific model override (e.g., gpt-5-nano-2025-08-07)")
    submit_parser.add_argument(
        "--provider-override",
        choices=["openai", "anthropic", "google", "groq", "xai", "deepseek", "qwen", "zai", "ollama", "mistral", "cohere"],
        help="Force a specific provider",
    )
    submit_parser.add_argument(
        "--mode",
        choices=["simple", "standard", "complex", "supervisor"],
        help="Execution mode hint",
    )
    # Strategy presets and tuning (Phase 6)
    submit_parser.add_argument(
        "--force-research",
        action="store_true",
        help="Force routing to ResearchWorkflow (for citation-enabled research)",
    )
    submit_parser.add_argument(
        "--research-strategy",
        choices=["quick", "standard", "deep", "academic"],
        help="Research strategy preset",
    )
    submit_parser.add_argument(
        "--max-iterations",
        type=int,
        help="Override max iterations for research agent (1..50)",
    )
    submit_parser.add_argument(
        "--max-concurrent-agents",
        type=int,
        help="Override max concurrent agents (1..20)",
    )
    ev_group = submit_parser.add_mutually_exclusive_group()
    ev_group.add_argument(
        "--enable-verification",
        dest="enable_verification",
        action="store_true",
        help="Enable claim verification",
    )
    ev_group.add_argument(
        "--disable-verification",
        dest="enable_verification",
        action="store_false",
        help="Disable claim verification",
    )
    submit_parser.set_defaults(enable_verification=None)
    

    # Citation toggle
    cit_group = submit_parser.add_mutually_exclusive_group()
    cit_group.add_argument(
        "--enable-citations",
        dest="enable_citations",
        action="store_true",
        help="Enable citation collection/integration (React/DAG opt-in)",
    )
    cit_group.add_argument(
        "--disable-citations",
        dest="enable_citations",
        action="store_false",
        help="Disable citation collection/integration",
    )
    submit_parser.set_defaults(enable_citations=None)

    # Status command
    status_parser = subparsers.add_parser("status", help="Get task status")
    status_parser.add_argument("task_id", help="Task ID")

    # Cancel command
    cancel_parser = subparsers.add_parser("cancel", help="Cancel a task")
    cancel_parser.add_argument("task_id", help="Task ID")
    cancel_parser.add_argument("--reason", help="Cancellation reason")

    # Pause command
    pause_parser = subparsers.add_parser("pause", help="Pause a task at checkpoints")
    pause_parser.add_argument("task_id", help="Task ID")
    pause_parser.add_argument("--reason", help="Pause reason")

    # Resume command
    resume_parser = subparsers.add_parser("resume", help="Resume a paused task")
    resume_parser.add_argument("task_id", help="Task ID")
    resume_parser.add_argument("--reason", help="Resume reason")

    # Stream command
    stream_parser = subparsers.add_parser("stream", help="Stream task events")
    stream_parser.add_argument("workflow_id", help="Workflow ID")
    stream_parser.add_argument(
        "--types",
        help="Event types to filter (comma-separated)",
    )
    # SSE is the only transport
    stream_parser.add_argument("--traceparent", help="W3C traceparent header for distributed tracing")

    # Control state command
    control_state_parser = subparsers.add_parser(
        "control-state", help="Get pause/cancel control state for a task"
    )
    control_state_parser.add_argument("task_id", help="Task ID")

    # Approve command
    approve_parser = subparsers.add_parser("approve", help="Approve pending request")
    approve_parser.add_argument("approval_id", help="Approval ID")
    approve_parser.add_argument("workflow_id", help="Workflow ID")
    approve_group = approve_parser.add_mutually_exclusive_group()
    approve_group.add_argument("--approve", action="store_true", dest="approved", default=True, help="Approve the request (default)")
    approve_group.add_argument("--reject", action="store_false", dest="approved", help="Reject the request")
    approve_parser.add_argument("--feedback", help="Approval feedback")

    # Session commands (HTTP)
    sess_get = subparsers.add_parser("session-get", help="Get a session")
    sess_get.add_argument("session_id", help="Session ID")
    sess_get.add_argument("--no-history", action="store_true", help="Do not include history")

    sess_list = subparsers.add_parser("session-list", help="List sessions")
    sess_list.add_argument("--limit", type=int, default=50)
    sess_list.add_argument("--offset", type=int, default=0)

    sess_delete = subparsers.add_parser("session-delete", help="Delete a session")
    sess_delete.add_argument("session_id", help="Session ID")
    # Optional: session title update
    sess_title = subparsers.add_parser("session-title", help="Update session title")
    sess_title.add_argument("session_id", help="Session ID")
    sess_title.add_argument("title", help="New title")

    # Schedule commands
    sched_create = subparsers.add_parser("schedule-create", help="Create a scheduled task")
    sched_create.add_argument("name", help="Schedule name")
    sched_create.add_argument("cron", help="Cron expression (e.g., '0 9 * * 1-5' for weekdays at 9am)")
    sched_create.add_argument("query", help="Task query to execute")
    sched_create.add_argument("--description", help="Schedule description")
    sched_create.add_argument("--timezone", default="UTC", help="Timezone (default: UTC)")
    sched_create.add_argument("--force-research", action="store_true", help="Enable deep research mode")
    sched_create.add_argument(
        "--research-strategy",
        choices=["quick", "standard", "deep", "academic"],
        help="Research strategy preset",
    )
    sched_create.add_argument("--budget", type=float, help="Max budget per run (USD)")
    sched_create.add_argument("--timeout", type=int, help="Timeout per run (seconds)")

    sched_list = subparsers.add_parser("schedule-list", help="List schedules")
    sched_list.add_argument("--page", type=int, default=1, help="Page number")
    sched_list.add_argument("--page-size", type=int, default=20, help="Items per page")
    sched_list.add_argument("--status", choices=["ACTIVE", "PAUSED"], help="Filter by status")

    sched_get = subparsers.add_parser("schedule-get", help="Get schedule details")
    sched_get.add_argument("schedule_id", help="Schedule ID")

    sched_update = subparsers.add_parser("schedule-update", help="Update a schedule")
    sched_update.add_argument("schedule_id", help="Schedule ID")
    sched_update.add_argument("--name", help="New name")
    sched_update.add_argument("--description", help="New description")
    sched_update.add_argument("--cron", help="New cron expression")
    sched_update.add_argument("--timezone", help="New timezone")
    sched_update.add_argument("--query", help="New task query")
    sched_update.add_argument("--budget", type=float, help="New max budget per run (USD)")
    sched_update.add_argument("--timeout", type=int, help="New timeout per run (seconds)")
    sched_update.add_argument("--clear-context", action="store_true", help="Clear task context")

    sched_pause = subparsers.add_parser("schedule-pause", help="Pause a schedule")
    sched_pause.add_argument("schedule_id", help="Schedule ID")
    sched_pause.add_argument("--reason", help="Pause reason")

    sched_resume = subparsers.add_parser("schedule-resume", help="Resume a paused schedule")
    sched_resume.add_argument("schedule_id", help="Schedule ID")
    sched_resume.add_argument("--reason", help="Resume reason")

    sched_delete = subparsers.add_parser("schedule-delete", help="Delete a schedule")
    sched_delete.add_argument("schedule_id", help="Schedule ID")

    sched_runs = subparsers.add_parser("schedule-runs", help="Get schedule execution history")
    sched_runs.add_argument("schedule_id", help="Schedule ID")
    sched_runs.add_argument("--page", type=int, default=1, help="Page number")
    sched_runs.add_argument("--page-size", type=int, default=10, help="Items per page")

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(1)

    # Initialize client
    client = ShannonClient(
        base_url=args.base_url,
        api_key=args.api_key if args.api_key else None,
        bearer_token=args.bearer_token if args.bearer_token else None,
    )

    try:
        if args.command == "submit":
            # Build context from strategy flags
            submit_ctx = {}
            if args.force_research:
                submit_ctx["force_research"] = True
            if args.research_strategy:
                submit_ctx["research_strategy"] = args.research_strategy
            if args.max_iterations is not None:
                submit_ctx["max_iterations"] = args.max_iterations
            if args.max_concurrent_agents is not None:
                submit_ctx["max_concurrent_agents"] = args.max_concurrent_agents
            if args.enable_verification is not None:
                submit_ctx["enable_verification"] = args.enable_verification
            if args.enable_citations is not None:
                submit_ctx["enable_citations"] = args.enable_citations
            

            handle = client.submit_task(
                args.query,
                session_id=args.session_id,
                context=submit_ctx or None,
                idempotency_key=args.idempotency_key,
                traceparent=args.traceparent,
                model_tier=args.model_tier,
                model_override=args.model_override,
                provider_override=args.provider_override,
                mode=args.mode,
            )
            print(f"Task submitted:")
            print(f"  Task ID: {handle.task_id}")
            print(f"  Workflow ID: {handle.workflow_id}")

            if args.wait:
                print("\nWaiting for completion...")
                status = client.wait(handle.task_id)

                if status.status == TaskStatusEnum.COMPLETED:
                    print(f"\n✓ Result: {status.result}")
                else:
                    print(f"\n✗ {status.status.value}: {status.error_message}")
                    sys.exit(1)

        elif args.command == "status":
            status = client.get_status(args.task_id)
            print(f"Task: {status.task_id}")
            print(f"Status: {status.status.value}")
            print(f"Progress: {status.progress:.1%}")
            if status.result:
                print(f"Result: {status.result}")
            if status.error_message:
                print(f"Error: {status.error_message}")

        elif args.command == "cancel":
            success = client.cancel(args.task_id, reason=args.reason)
            if success:
                print(f"✓ Task {args.task_id} cancelled")
            else:
                print(f"✗ Failed to cancel task {args.task_id}")
                sys.exit(1)

        elif args.command == "pause":
            success = client.pause_task(args.task_id, reason=args.reason)
            if success:
                print(f"✓ Pause request sent for task {args.task_id} (will take effect at next checkpoint)")
            else:
                print(f"✗ Failed to send pause request for task {args.task_id}")
                sys.exit(1)

        elif args.command == "resume":
            success = client.resume_task(args.task_id, reason=args.reason)
            if success:
                print(f"✓ Resume request sent for task {args.task_id}")
            else:
                print(f"✗ Failed to send resume request for task {args.task_id}")
                sys.exit(1)

        elif args.command == "control-state":
            state = client.get_control_state(args.task_id)
            print(f"Task: {args.task_id}")
            print(f"Paused: {state.is_paused}")
            print(f"Cancelled: {state.is_cancelled}")
            if state.paused_at:
                print(f"Paused at: {state.paused_at.isoformat()}")
            if state.pause_reason:
                print(f"Pause reason: {state.pause_reason}")
            if state.paused_by:
                print(f"Paused by: {state.paused_by}")
            if state.cancel_reason:
                print(f"Cancel reason: {state.cancel_reason}")
            if state.cancelled_by:
                print(f"Cancelled by: {state.cancelled_by}")

        elif args.command == "stream":
            # Parse event types filter
            event_types = None
            if args.types:
                event_types = [t.strip() for t in args.types.split(",")]

            print(f"Streaming events for workflow: {args.workflow_id}")
            print("-" * 60)

            try:
                for event in client.stream(
                    args.workflow_id,
                    types=event_types,
                    traceparent=args.traceparent,
                ):
                    timestamp = event.timestamp.strftime("%H:%M:%S")
                    agent = f"[{event.agent_id}] " if event.agent_id else ""
                    print(f"{timestamp} {agent}{event.type}: {event.message}")

                    # Exit on completion
                    if event.type == EventType.WORKFLOW_COMPLETED.value:
                        break

            except KeyboardInterrupt:
                print("\n\nStream interrupted by user")
            except Exception as e:
                print(f"\n✗ Stream error: {e}")
                sys.exit(1)

        elif args.command == "approve":
            success = client.approve(
                approval_id=args.approval_id,
                workflow_id=args.workflow_id,
                approved=args.approved,
                feedback=args.feedback,
            )
            if success:
                action = "approved" if args.approved else "rejected"
                print(f"✓ Request {action}")
            else:
                print(f"✗ Failed to submit approval")
                sys.exit(1)

        elif args.command == "session-create":
            print("This command is no longer supported in HTTP SDK.")
            sys.exit(1)

        elif args.command == "session-get":
            sess = client.get_session(args.session_id)
            print(f"Session {sess.session_id} (user={sess.user_id})")
            print(f"Created: {sess.created_at}, Updated: {sess.updated_at}")
            if not args.no_history:
                try:
                    hist = client.get_session_history(args.session_id)
                    print(f"History msgs: {len(hist)}")
                except Exception:
                    print("History not available")

        elif args.command == "session-list":
            sessions, total = client.list_sessions(limit=args.limit, offset=args.offset)
            print(f"Total: {total}")
            for s in sessions:
                print(f"{s.session_id}\t{s.user_id}\t{s.created_at}")

        elif args.command == "session-delete":
            ok = client.delete_session(args.session_id)
            print("✓ Deleted" if ok else "✗ Delete failed")

        elif args.command == "session-title":
            ok = client.update_session_title(args.session_id, args.title)
            print("✓ Updated" if ok else "✗ Update failed")

        elif args.command == "schedule-create":
            # Build task context
            task_ctx = {}
            if args.force_research:
                task_ctx["force_research"] = "true"
            if args.research_strategy:
                task_ctx["research_strategy"] = args.research_strategy

            result = client.create_schedule(
                name=args.name,
                cron_expression=args.cron,
                task_query=args.query,
                description=args.description,
                timezone=args.timezone,
                task_context=task_ctx if task_ctx else None,
                max_budget_per_run_usd=args.budget,
                timeout_seconds=args.timeout,
            )
            print(f"✓ Schedule created:")
            print(f"  ID: {result.get('schedule_id')}")
            print(f"  Next run: {result.get('next_run_at')}")

        elif args.command == "schedule-list":
            schedules, total = client.list_schedules(
                page=args.page, page_size=args.page_size, status=args.status
            )
            print(f"Total: {total}")
            for s in schedules:
                status_icon = "▶" if s.status == "ACTIVE" else "⏸"
                print(f"{status_icon} {s.schedule_id}\t{s.name}\t{s.cron_expression}\t{s.status}")

        elif args.command == "schedule-get":
            sched = client.get_schedule(args.schedule_id)
            print(f"Schedule: {sched.name}")
            print(f"  ID: {sched.schedule_id}")
            print(f"  Status: {sched.status}")
            print(f"  Cron: {sched.cron_expression}")
            print(f"  Timezone: {sched.timezone or 'UTC'}")
            print(f"  Query: {sched.task_query}")
            if sched.task_context:
                print(f"  Context: {sched.task_context}")
            if sched.next_run_at:
                print(f"  Next run: {sched.next_run_at}")
            if sched.last_run_at:
                print(f"  Last run: {sched.last_run_at}")
            print(f"  Runs: {sched.total_runs} total, {sched.successful_runs} succeeded, {sched.failed_runs} failed")

        elif args.command == "schedule-update":
            result = client.update_schedule(
                args.schedule_id,
                name=args.name,
                description=args.description,
                cron_expression=args.cron,
                timezone=args.timezone,
                task_query=args.query,
                max_budget_per_run_usd=args.budget,
                timeout_seconds=args.timeout,
                clear_task_context=args.clear_context,
            )
            print("✓ Schedule updated")
            if result.get("next_run_at"):
                print(f"  Next run: {result.get('next_run_at')}")

        elif args.command == "schedule-pause":
            ok = client.pause_schedule(args.schedule_id, reason=args.reason)
            print("✓ Schedule paused" if ok else "✗ Pause failed")

        elif args.command == "schedule-resume":
            ok = client.resume_schedule(args.schedule_id, reason=args.reason)
            print("✓ Schedule resumed" if ok else "✗ Resume failed")

        elif args.command == "schedule-delete":
            ok = client.delete_schedule(args.schedule_id)
            print("✓ Schedule deleted" if ok else "✗ Delete failed")

        elif args.command == "schedule-runs":
            runs, total = client.get_schedule_runs(
                args.schedule_id, page=args.page, page_size=args.page_size
            )
            print(f"Total runs: {total}")
            for run in runs:
                status_icon = "✓" if run.status == "COMPLETED" else "✗" if run.status == "FAILED" else "⏳"
                cost = f"${run.total_cost_usd:.4f}" if run.total_cost_usd else "-"
                print(f"{status_icon} {run.triggered_at}\t{run.status}\t{run.total_tokens} tokens\t{cost}")

    except errors.ShannonError as e:
        print(f"✗ {e}")
        sys.exit(1)
    except Exception as e:
        print(f"✗ Unexpected error: {e}")
        sys.exit(1)
    finally:
        client.close()


if __name__ == "__main__":
    main()
