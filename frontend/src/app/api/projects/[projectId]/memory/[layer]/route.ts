import { clearMemory } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string; layer: string }> };
export async function DELETE(request: Request, { params }: Context) { const value = await params; if (value.layer !== "global" && value.layer !== "project") return Response.json({ error: { category: "validation", message: "Некорректный запрос." } }, { status: 400 }); return clearMemory(request, value.projectId, value.layer); }
