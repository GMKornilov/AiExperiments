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
  const step = body.messages.filter((message) => message.role === "user").length;
  if (!titleRequest && (text.startsWith("tokens-overflow") || (text.startsWith("tokens-growth") && step > 8))) {
    response.writeHead(400, { "Content-Type": "application/json" }).end(JSON.stringify({
      error: { code: "context_length_exceeded", message: "This model's maximum context length is exceeded." },
    }));
    return;
  }
  let usage = titleRequest
    ? { prompt_tokens: 9000, completion_tokens: 1000, total_tokens: 10000 }
    : { prompt_tokens: 50 + 50 * step, completion_tokens: 10 + 10 * step, total_tokens: 60 + 60 * step };
  if (text.startsWith("tokens-missing")) usage = undefined;
  if (text.startsWith("tokens-invalid")) usage = { prompt_tokens: -1, completion_tokens: 20, total_tokens: 19 };
  if (text.startsWith("tokens-zero")) usage = { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 };
  if (!titleRequest && text.startsWith("tokens-empty-retry") && count === 1) {
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [], usage }));
    return;
  }
  const answer = titleRequest ? "Название диалога" : `Ответ: ${text} [${body.model}]`;
  response.writeHead(200, { "Content-Type": "application/json" });
  response.end(JSON.stringify({ choices: [{ message: { role: "assistant", content: answer } }], usage }));
}).listen(port, "127.0.0.1", () => console.log(`fixture provider on ${port}`));
