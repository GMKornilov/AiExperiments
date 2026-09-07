import { createServer } from "node:http";

const port = Number(process.env.BARISTA_PROVIDER_PORT ?? 18081);
const seen = new Map();

createServer(async (request, response) => {
  if (request.method !== "POST") { response.writeHead(405).end(); return; }
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  let body;
  try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { response.writeHead(400).end(); return; }
  const text = body?.messages?.at(-1)?.content;
  if (typeof text !== "string") { response.writeHead(400).end(); return; }
  const titleRequest = body.model === "e2e-title-model";
  const key = `${body.model}:${text}`;
  const count = (seen.get(key) ?? 0) + 1;
  seen.set(key, count);
  if ((titleRequest && text.includes("title-slow")) || (!titleRequest && text.startsWith("slow"))) await new Promise((resolve) => setTimeout(resolve, titleRequest ? 4000 : 8000));
  if (!titleRequest && text.startsWith("fail") && count === 1) { response.writeHead(503).end("fixture provider failure"); return; }
  const answer = titleRequest ? "Название диалога" : `Ответ: ${text} [${body.model}]`;
  response.writeHead(200, { "Content-Type": "application/json" });
  response.end(JSON.stringify({ choices: [{ message: { role: "assistant", content: answer } }] }));
}).listen(port, "127.0.0.1", () => console.log(`fixture provider on ${port}`));
