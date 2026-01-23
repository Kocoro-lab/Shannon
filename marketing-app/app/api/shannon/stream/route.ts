import { NextRequest } from "next/server";

// Shannon API URL - server-side only
const SHANNON_API_URL = process.env.SHANNON_API_URL || "http://localhost:8080";

export async function GET(request: NextRequest) {
  const workflowId = request.nextUrl.searchParams.get("workflow_id");
  const lastEventId = request.nextUrl.searchParams.get("last_event_id");

  if (!workflowId) {
    return new Response(JSON.stringify({ error: "workflow_id is required" }), {
      status: 400,
      headers: { "Content-Type": "application/json" },
    });
  }

  // Build SSE URL to Shannon backend
  let sseUrl = `${SHANNON_API_URL}/api/v1/stream/sse?workflow_id=${encodeURIComponent(
    workflowId
  )}`;

  if (lastEventId) {
    sseUrl += `&last_event_id=${encodeURIComponent(lastEventId)}`;
  }

  // Add authentication
  const authHeader = request.headers.get("authorization");
  const userId = process.env.NEXT_PUBLIC_USER_ID;

  const headers: Record<string, string> = {
    Accept: "text/event-stream",
    "Cache-Control": "no-cache",
    Connection: "keep-alive",
  };

  if (authHeader) {
    // Extract token from Bearer header and add as query param (SSE limitation)
    const token = authHeader.replace("Bearer ", "");
    sseUrl += `&token=${encodeURIComponent(token)}`;
  } else if (userId) {
    headers["X-User-Id"] = userId;
  }

  try {
    const response = await fetch(sseUrl, {
      headers,
    });

    if (!response.ok) {
      const errorText = await response.text();
      return new Response(
        JSON.stringify({ error: `Upstream error: ${errorText}` }),
        {
          status: response.status,
          headers: { "Content-Type": "application/json" },
        }
      );
    }

    // Create a TransformStream to proxy the SSE
    const { readable, writable } = new TransformStream();
    const writer = writable.getWriter();
    const reader = response.body?.getReader();

    if (!reader) {
      return new Response(JSON.stringify({ error: "No response body" }), {
        status: 500,
        headers: { "Content-Type": "application/json" },
      });
    }

    // Pipe the upstream SSE to the client
    (async () => {
      try {
        while (true) {
          const { done, value } = await reader.read();
          if (done) break;
          await writer.write(value);
        }
      } catch (error) {
        console.error("SSE proxy error:", error);
      } finally {
        try {
          await writer.close();
        } catch {
          // Ignore close errors
        }
      }
    })();

    return new Response(readable, {
      headers: {
        "Content-Type": "text/event-stream",
        "Cache-Control": "no-cache",
        Connection: "keep-alive",
        "X-Accel-Buffering": "no",
      },
    });
  } catch (error) {
    console.error("SSE proxy connection error:", error);
    return new Response(
      JSON.stringify({ error: "Failed to connect to stream" }),
      {
        status: 502,
        headers: { "Content-Type": "application/json" },
      }
    );
  }
}
