import { pauseTask } from "@/lib/server/barista";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string; chatId: string; taskId: string }> };

export async function POST(request: Request, { params }: Context) {
  const value = await params;
  return pauseTask(request, value.projectId, value.chatId, value.taskId);
}
