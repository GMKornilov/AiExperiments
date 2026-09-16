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
  if (body.model === "e2e-facts-model") {
    if (text.includes("facts-error")) { response.writeHead(503).end("controlled facts failure"); return; }
    if (text.includes("facts-invalid")) {
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({
        choices: [{ message: { content: "[]" } }], usage: { prompt_tokens: 7, completion_tokens: 3 },
      }));
      return;
    }
    let latest = "";
    try {
      const payload = JSON.parse(text);
      latest = [...(payload.messages ?? [])].reverse().find((message) => message?.role === "user")?.content ?? "";
    } catch { response.writeHead(400).end("invalid facts payload"); return; }
    const facts = latest.includes("доза теперь 17") ? { dose: "17 г" } : { latest_user: String(latest).slice(0, 120) };
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({
      choices: [{ message: { content: JSON.stringify(facts) } }], usage: { prompt_tokens: 7, completion_tokens: 3 },
    }));
    return;
  }
  if (body.model === "memory-extractor") {
    if (text.includes("memory-error")) { response.writeHead(503).end("controlled memory failure"); return; }
    if (text.includes("memory-slow")) await new Promise((resolve) => setTimeout(resolve, 4000));
    if (text.includes("memory-invalid")) {
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [{ message: { content: "not-json" } }], usage: { prompt_tokens: 7, completion_tokens: 3 } }));
      return;
    }
    let payload;
    try { payload = JSON.parse(text); } catch { response.writeHead(400).end("invalid memory payload"); return; }
    const latest = [...(payload?.messages ?? [])].reverse().find((message) => message?.role === "user")?.text ?? "";
    // The extractor contract returns complete snapshots. Preserve prior facts unless
    // the user explicitly says that a resource is unavailable or finished.
    let facts = [...(payload?.global_facts ?? [])];
    let projectFacts = [...(payload?.project_facts ?? [])];
    if (latest.includes("V60") && !facts.includes("Оборудование: V60")) facts.push("Оборудование: V60");
    if (latest.includes("Эфиоп") && !projectFacts.includes("Есть зёрна: Эфиопия")) projectFacts.push("Есть зёрна: Эфиопия");
    if (latest.includes("V60 больше нет")) facts = facts.filter((fact) => fact !== "Оборудование: V60");
    if (latest.includes("Эфиопия закончилась")) projectFacts = projectFacts.filter((fact) => fact !== "Есть зёрна: Эфиопия");
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ global_facts: facts, project_facts: projectFacts }) } }], usage: { prompt_tokens: 7, completion_tokens: 3 } }));
    return;
  }
  if (body.model === "e2e-summary-model") {
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({
      choices: [{ message: { content: "Пользователь предпочитает кофе без молока; доза 18 г." } }],
      usage: { prompt_tokens: 40, completion_tokens: 10 },
    }));
    return;
  }
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
