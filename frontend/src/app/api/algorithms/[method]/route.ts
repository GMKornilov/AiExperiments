import { algorithmMethods, proxyAlgorithmRequest, type AlgorithmMethod } from "@/lib/server/algorithms";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = { params: Promise<{ method: string }> };

export async function POST(request: Request, { params }: RouteContext) {
  const { method } = await params;
  if (!algorithmMethods.includes(method as AlgorithmMethod)) {
    return Response.json({ error: "Неизвестный метод." }, { status: 404 });
  }
  return proxyAlgorithmRequest(request, method as AlgorithmMethod);
}
