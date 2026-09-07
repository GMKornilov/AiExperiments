import { retryMessage } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ id: string; messageId: string }> };
export async function POST(request: Request, { params }: Context) { const { id, messageId } = await params; return retryMessage(request, id, messageId); }
