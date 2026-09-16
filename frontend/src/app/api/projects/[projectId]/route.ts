import { deleteProject, renameProject } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ projectId: string }> };
export async function DELETE(request: Request, { params }: Context) { return deleteProject(request, (await params).projectId); }
export async function PATCH(request: Request, { params }: Context) { return renameProject(request, (await params).projectId); }
