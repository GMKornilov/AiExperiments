import { adminLogs } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
export async function GET(request: Request) { return adminLogs(request); }
