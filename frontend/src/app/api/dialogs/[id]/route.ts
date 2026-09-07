import { deleteDialog, getDialog } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ id: string }> };
export async function GET(request: Request, { params }: Context) { return getDialog(request, (await params).id); }
export async function DELETE(request: Request, { params }: Context) { return deleteDialog(request, (await params).id); }
