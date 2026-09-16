import { createProject, listProjects } from "@/lib/server/barista";
export const runtime = "nodejs"; export const dynamic = "force-dynamic";
export async function GET(request: Request) { return listProjects(request); }
export async function POST(request: Request) { return createProject(request); }
