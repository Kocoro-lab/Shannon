"""Shannon CLI tool."""

import argparse
import os
import sys
import time

from shannon import ShannonClient, TaskStatusEnum, EventType


def main():
    """Main CLI entry point."""
    parser = argparse.ArgumentParser(description="Shannon AI Platform CLI")
    parser.add_argument(
        "--endpoint",
        default=os.getenv("SHANNON_GRPC_ENDPOINT", "localhost:50052"),
        help="gRPC endpoint (default: localhost:50052)",
    )
    parser.add_argument(
        "--http-endpoint",
        default=os.getenv("SHANNON_HTTP_ENDPOINT", "http://localhost:8081"),
        help="HTTP endpoint for SSE (default: http://localhost:8081)",
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
    submit_parser.add_argument("--user-id", default="cli-user", help="User ID")
    submit_parser.add_argument("--session-id", help="Session ID")
    submit_parser.add_argument("--wait", action="store_true", help="Wait for completion")

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
    stream_parser.add_argument("--use-sse", action="store_true", help="Force SSE transport")

    # Approve command
    approve_parser = subparsers.add_parser("approve", help="Approve pending request")
    approve_parser.add_argument("approval_id", help="Approval ID")
    approve_parser.add_argument("workflow_id", help="Workflow ID")
    approve_group = approve_parser.add_mutually_exclusive_group()
    approve_group.add_argument("--approve", action="store_true", dest="approved", default=True, help="Approve the request (default)")
    approve_group.add_argument("--reject", action="store_false", dest="approved", help="Reject the request")
    approve_parser.add_argument("--feedback", help="Approval feedback")

    # List approvals command
    list_approvals_parser = subparsers.add_parser("approvals", help="List pending approvals")
    list_approvals_parser.add_argument("--user-id", help="Filter by user ID")
    list_approvals_parser.add_argument("--session-id", help="Filter by session ID")

    # Session commands (basic)
    sess_create = subparsers.add_parser("session-create", help="Create a session")
    sess_create.add_argument("--user-id", required=True, help="User ID")
    sess_create.add_argument("--ttl-seconds", type=int, default=3600, help="TTL seconds")
    sess_create.add_argument("--max-history", type=int, default=50, help="Max history size")

    sess_get = subparsers.add_parser("session-get", help="Get a session")
    sess_get.add_argument("session_id", help="Session ID")
    sess_get.add_argument("--no-history", action="store_true", help="Do not include history")

    sess_list = subparsers.add_parser("session-list", help="List sessions for a user")
    sess_list.add_argument("--user-id", required=True, help="User ID")
    sess_list.add_argument("--all", action="store_true", help="Include inactive sessions")
    sess_list.add_argument("--limit", type=int, default=50)
    sess_list.add_argument("--offset", type=int, default=0)

    sess_delete = subparsers.add_parser("session-delete", help="Delete a session")
    sess_delete.add_argument("session_id", help="Session ID")

    sess_add_msg = subparsers.add_parser("session-add-message", help="Add message to session")
    sess_add_msg.add_argument("session_id", help="Session ID")
    sess_add_msg.add_argument("--role", default="user", choices=["user", "assistant", "system"], help="Message role")
    sess_add_msg.add_argument("--content", required=True, help="Message content")

    sess_clear = subparsers.add_parser("session-clear", help="Clear session history")
    sess_clear.add_argument("session_id", help="Session ID")
    sess_clear.add_argument("--drop-context", action="store_true", help="Also drop persistent context")

    sess_update = subparsers.add_parser("session-update", help="Update a session (TTL only)")
    sess_update.add_argument("session_id", help="Session ID")
    sess_update.add_argument("--extend-ttl", type=int, default=0, help="Seconds to extend TTL")

    args = parser.parse_args()

    if not args.command:
        parser.print_help()
        sys.exit(1)

    # Initialize client
    client = ShannonClient(
        grpc_endpoint=args.endpoint,
        http_endpoint=args.http_endpoint,
        api_key=args.api_key if args.api_key else None,
        bearer_token=args.bearer_token if args.bearer_token else None,
    )

    try:
        if args.command == "submit":
            handle = client.submit_task(
                args.query,
                user_id=args.user_id,
                session_id=args.session_id,
            )
            print(f"Task submitted:")
            print(f"  Task ID: {handle.task_id}")
            print(f"  Workflow ID: {handle.workflow_id}")

            if args.wait:
                print("\nWaiting for completion...")
                while True:
                    status = client.get_status(handle.task_id)
                    print(f"  Status: {status.status.value} ({status.progress:.1%})")

                    if status.status in [
                        TaskStatusEnum.COMPLETED,
                        TaskStatusEnum.FAILED,
                        TaskStatusEnum.CANCELLED,
                        TaskStatusEnum.TIMEOUT,
                    ]:
                        break
                    time.sleep(2)

                if status.status == TaskStatusEnum.COMPLETED:
                    print(f"\n✓ Result: {status.result}")
                else:
                    print(f"\n✗ {status.status.value}: {status.error_message}")
                    sys.exit(1)

        elif args.command == "status":
            status = client.get_status(args.task_id, include_details=True)
            print(f"Task: {status.task_id}")
            print(f"Status: {status.status.value}")
            print(f"Progress: {status.progress:.1%}")
            if status.result:
                print(f"Result: {status.result}")
            if status.error_message:
                print(f"Error: {status.error_message}")
            if status.metrics:
                print(f"Tokens: {status.metrics.tokens_used}")
                print(f"Cost: ${status.metrics.cost_usd:.4f}")

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

            use_grpc = None if not args.use_sse else False

            print(f"Streaming events for workflow: {args.workflow_id}")
            print("-" * 60)

            try:
                for event in client.stream(
                    args.workflow_id,
                    types=event_types,
                    use_grpc=use_grpc,
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

        elif args.command == "approvals":
            approvals = client.get_pending_approvals(
                user_id=args.user_id,
                session_id=args.session_id,
            )
            if approvals:
                print(f"Found {len(approvals)} pending approval(s):\n")
                for approval in approvals:
                    print(f"  Approval ID: {approval.approval_id}")
                    print(f"  Workflow: {approval.workflow_id}")
                    print(f"  Message: {approval.message}")
                    print(f"  Requested: {approval.requested_at}")
                    print()
            else:
                print("No pending approvals")

        elif args.command == "session-create":
            sess = client.create_session(
                args.user_id,
                ttl_seconds=args.ttl_seconds,
                max_history=args.max_history,
            )
            print(f"✓ Session created: {sess.session_id}")

        elif args.command == "session-get":
            sess = client.get_session(args.session_id, include_history=(not args.no_history))
            print(f"Session {sess.session_id} (user={sess.user_id})")
            print(f"Created: {sess.created_at}, Updated: {sess.updated_at}")
            print(f"History msgs: {len(sess.history)}")

        elif args.command == "session-list":
            sessions = client.list_sessions(
                args.user_id,
                active_only=not args.all,  # Default True (active only), --all makes it False
                limit=args.limit,
                offset=args.offset,
            )
            if sessions:
                for s in sessions:
                    print(f"{s.session_id}\t{s.user_id}\t{s.updated_at}\tmsgs={s.message_count}")
            else:
                print("No sessions found")

        elif args.command == "session-delete":
            ok = client.delete_session(args.session_id)
            print("✓ Deleted" if ok else "✗ Delete failed")

        elif args.command == "session-add-message":
            ok = client.add_message(args.session_id, role=args.role, content=args.content)
            print("✓ Added" if ok else "✗ Add failed")

        elif args.command == "session-clear":
            ok = client.clear_history(args.session_id, keep_context=(not args.drop_context))
            print("✓ Cleared" if ok else "✗ Clear failed")

        elif args.command == "session-update":
            ok = client.update_session(args.session_id, extend_ttl_seconds=args.extend_ttl or 0)
            print("✓ Updated" if ok else "✗ Update failed")

    finally:
        client.close()


if __name__ == "__main__":
    main()
