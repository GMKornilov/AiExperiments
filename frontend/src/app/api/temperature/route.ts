import { proxyTemperatureRequest } from "@/lib/server/temperature";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  return proxyTemperatureRequest(request);
}
