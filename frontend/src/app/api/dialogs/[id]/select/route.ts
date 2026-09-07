import { selectDialog } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
type Context = { params: Promise<{ id: string }> };
export async function POST(request: Request, { params }: Context) { return selectDialog(request, (await params).id); }
