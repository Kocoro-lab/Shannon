import { NextRequest, NextResponse } from "next/server";

// Shannon API URL - server-side only (never exposed to client)
const SHANNON_API_URL = process.env.SHANNON_API_URL || "http://localhost:8080";

// Rate limiting (simple in-memory implementation)
const requestCounts = new Map<string, { count: number; resetAt: number }>();
const RATE_LIMIT = 100; // requests per minute
const RATE_WINDOW = 60 * 1000; // 1 minute

function checkRateLimit(ip: string): boolean {
  const now = Date.now();
  const record = requestCounts.get(ip);

  if (!record || now > record.resetAt) {
    requestCounts.set(ip, { count: 1, resetAt: now + RATE_WINDOW });
    return true;
  }

  if (record.count >= RATE_LIMIT) {
    return false;
  }

  record.count++;
  return true;
}

function getClientIP(request: NextRequest): string {
  const forwardedFor = request.headers.get("x-forwarded-for");
  if (forwardedFor) {
    return forwardedFor.split(",")[0].trim();
  }
  return "unknown";
}

// Handle all HTTP methods
async function handler(
  request: NextRequest,
  { params }: { params: Promise<{ path: string[] }> }
) {
  const { path } = await params;
  const clientIP = getClientIP(request);

  // Rate limit check
  if (!checkRateLimit(clientIP)) {
    return NextResponse.json(
      { error: "Too many requests" },
      { status: 429 }
    );
  }

  // Build the target URL
  const targetPath = path.join("/");
  const searchParams = request.nextUrl.searchParams.toString();
  const targetUrl = `${SHANNON_API_URL}/api/v1/${targetPath}${
    searchParams ? `?${searchParams}` : ""
  }`;

  try {
    // Forward the request to Shannon backend
    const headers: Record<string, string> = {
      "Content-Type": "application/json",
    };

    // Forward authentication headers if present
    const authHeader = request.headers.get("authorization");
    if (authHeader) {
      headers["Authorization"] = authHeader;
    }

    // For development: add default user ID if no auth
    if (!authHeader) {
      const userId = process.env.NEXT_PUBLIC_USER_ID;
      if (userId) {
        headers["X-User-Id"] = userId;
      }
    }

    let body: string | undefined;
    if (request.method !== "GET" && request.method !== "HEAD") {
      body = await request.text();
    }

    const response = await fetch(targetUrl, {
      method: request.method,
      headers,
      body,
    });

    // Get response data
    const responseText = await response.text();
    let responseData;
    try {
      responseData = JSON.parse(responseText);
    } catch {
      responseData = responseText;
    }

    // Return the response
    return NextResponse.json(responseData, {
      status: response.status,
      headers: {
        "X-Request-Id": response.headers.get("X-Request-Id") || "",
      },
    });
  } catch (error) {
    console.error("Shannon API proxy error:", error);
    return NextResponse.json(
      { error: "Failed to connect to backend service" },
      { status: 502 }
    );
  }
}

export const GET = handler;
export const POST = handler;
export const PUT = handler;
export const DELETE = handler;
export const PATCH = handler;
