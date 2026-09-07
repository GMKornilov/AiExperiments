import { createDialog, listDialogs } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
export async function GET(request: Request) { return listDialogs(request); }
export async function POST(request: Request) { return createDialog(request); }
