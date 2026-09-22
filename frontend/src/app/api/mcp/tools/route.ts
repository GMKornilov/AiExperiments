import { getMCPURL, listMCPTools, MCPError } from "@/lib/server/mcp";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const statuses = {
  MCP_NOT_CONFIGURED: 503,
  MCP_UNAVAILABLE: 503,
  MCP_TIMEOUT: 504,
  MCP_PROTOCOL_ERROR: 502,
  MCP_INVALID_RESPONSE: 502,
} as const;

const messages = {
  MCP_NOT_CONFIGURED: "MCP endpoint is not configured.",
  MCP_UNAVAILABLE: "MCP is temporarily unavailable.",
  MCP_TIMEOUT: "MCP did not respond in time.",
  MCP_PROTOCOL_ERROR: "Unable to connect to MCP.",
  MCP_INVALID_RESPONSE: "MCP returned an invalid response.",
} as const;

export async function POST(request: Request): Promise<Response> {
  const startedAt = Date.now();
  const correlationID = getCorrelationID(request);
  try {
    const tools = await listMCPTools(getMCPURL(), correlationID);
    logAttempt(correlationID, "success", undefined, startedAt);
    return Response.json({ tools });
  } catch (error) {
    const safeError = error instanceof MCPError
      ? error
      : new MCPError("MCP_PROTOCOL_ERROR", "Unable to connect to MCP.");
    logAttempt(correlationID, "failure", safeError.code, startedAt);
    return Response.json(
      { code: safeError.code, message: messages[safeError.code] },
      { status: statuses[safeError.code] },
    );
  }
}

function getCorrelationID(request: Request): string {
  const candidate = request.headers.get("x-request-id");
  if (candidate && /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/.test(candidate)) return candidate;
  return crypto.randomUUID();
}

function logAttempt(
  correlationID: string,
  outcome: "success" | "failure",
  errorCategory: keyof typeof statuses | undefined,
  startedAt: number,
): void {
  console.info(JSON.stringify({
    correlation_id: correlationID,
    operation: "mcp_tools_list",
    outcome,
    ...(errorCategory ? { error_category: errorCategory } : {}),
    duration_ms: Date.now() - startedAt,
  }));
}
