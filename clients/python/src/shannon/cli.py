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

    # Status command
    status_parser = subparsers.add_parser("status", help="Get task status")
    status_parser.add_argument("task_id", help="Task ID")

    # Cancel command
    cancel_parser = subparsers.add_parser("cancel", help="Cancel a task")
    cancel_parser.add_argument("task_id", help="Task ID")
    cancel_parser.add_argument("--reason", help="Cancellation reason")

    # Stream command
    stream_parser = subparsers.add_parser("stream", help="Stream task events")
    stream_parser.add_argument("workflow_id", help="Workflow ID")
    stream_parser.add_argument(
        "--types",
        help="Event types to filter (comma-separated)",
    )
    # SSE is the only transport
    stream_parser.add_argument("--traceparent", help="W3C traceparent header for distributed tracing")

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
            handle = client.submit_task(
                args.query,
                session_id=args.session_id,
                idempotency_key=args.idempotency_key,
                traceparent=args.traceparent,
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
