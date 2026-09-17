import { deleteProfile } from "@/lib/server/barista";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function DELETE(request: Request, { params }: { params: Promise<{ id: string }> }) {
  return deleteProfile(request, (await params).id);
}
