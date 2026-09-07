import { postEvent } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
export async function POST(request: Request) { return postEvent(request); }
