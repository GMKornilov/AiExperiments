import { deleteChat, getChat } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string; chatId: string }> };
export async function GET(request: Request, { params }: Context) { const value = await params; return getChat(request, value.projectId, value.chatId); }
export async function DELETE(request: Request, { params }: Context) { const value = await params; return deleteChat(request, value.projectId, value.chatId); }
