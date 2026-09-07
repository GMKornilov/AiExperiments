import { sendMessage } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ id: string }> };
export async function POST(request: Request, { params }: Context) { return sendMessage(request, (await params).id); }
