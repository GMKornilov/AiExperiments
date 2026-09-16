import { getMemory } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string }> };
export async function GET(request: Request, { params }: Context) { return getMemory(request, (await params).projectId); }
