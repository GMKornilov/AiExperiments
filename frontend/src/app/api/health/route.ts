import { checkBackendHealth } from "@/lib/server/backend";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET() {
  return checkBackendHealth();
}
