import { sendProjectMessage } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string; chatId: string }> };
export async function POST(request: Request, { params }: Context) { const value = await params; return sendProjectMessage(request, value.projectId, value.chatId); }
