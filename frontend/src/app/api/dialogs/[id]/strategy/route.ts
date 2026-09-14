import { setStrategy } from "@/lib/server/barista";
export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export async function PATCH(request: Request, { params }: { params: Promise<{ id: string }> }) { return setStrategy(request, (await params).id); }
