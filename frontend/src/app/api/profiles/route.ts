import { createProfile, listProfiles } from "@/lib/server/barista";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

export async function GET(request: Request) { return listProfiles(request); }
export async function POST(request: Request) { return createProfile(request); }
