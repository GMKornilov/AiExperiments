import { createChat } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string }> };
export async function POST(request: Request, { params }: Context) { return createChat(request, (await params).projectId); }
