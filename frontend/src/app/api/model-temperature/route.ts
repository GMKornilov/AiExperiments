import { proxyModelTemperatureRequest } from "@/lib/server/model-temperature";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function POST(request: Request) {
  return proxyModelTemperatureRequest(request);
}
