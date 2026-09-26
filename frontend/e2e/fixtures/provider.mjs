import { createServer } from "node:http";

const port = Number(process.env.BARISTA_PROVIDER_PORT ?? 18081);
const seen = new Map();
const toolSmoke = {
  tool_requests: 0, no_tool_requests: 0, continuations: 0, pairing_valid: true,
  catalogue_calls: 0, unexpected_tools: 0, research_proposals: 0,
  research_recipe_leaks: 0, execution_after_research: 0,
  execution_after_tool: 0, execution_after_no_tool: 0,
  batch_tool_requests: 0, batch_tool_calls: 0, ordered_batch_valid: true,
  grinder_catalogue_calls: 0, brewer_catalogue_calls: 0,
};

createServer(async (request, response) => {
  const url = new URL(request.url ?? "/", "http://fixture-provider");
  if (request.method === "GET") {
    if (url.pathname === "/__tool-state") {
      if (url.searchParams.get("reset") === "1") {
        Object.assign(toolSmoke, {
          tool_requests: 0, no_tool_requests: 0, continuations: 0, pairing_valid: true,
          catalogue_calls: 0, unexpected_tools: 0, research_proposals: 0,
          research_recipe_leaks: 0, execution_after_research: 0,
          execution_after_tool: 0, execution_after_no_tool: 0,
          batch_tool_requests: 0, batch_tool_calls: 0, ordered_batch_valid: true,
          grinder_catalogue_calls: 0, brewer_catalogue_calls: 0,
        });
      }
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify(toolSmoke));
      return;
    }
    const catalogue = {
      "/api/grinders": {
        grinders: [{ id: 1, brand: "Timemore", name: "C5 ESP Pro", minSetting: 0, maxSetting: 100, settingUnit: "CLICKS", espressoAnchor: 12, filterAnchor: 48, coarseAnchor: 80, mokaAnchor: 20, frenchPressAnchor: null, burrType: "CONICAL", createdAt: "2026-09-24T00:00:00Z" }],
        brands: ["Timemore"],
      },
      "/api/machines": {
        machines: [{ id: 2, brand: "DeLonghi", name: "EC685", brewMethod: "ESPRESSO", minBatchGrams: 7, maxBatchGrams: 18, createdAt: "2026-09-24T00:00:00Z" }],
        brands: ["DeLonghi"],
      },
      "/api/filters": {
        filters: [{ id: 3, name: "V60 Paper Filter 02", grindAdjustment: 1, createdAt: "2026-09-24T00:00:00Z" }],
      },
      "/api/brew-methods": {
        data: [{ id: "V60", label: "V60", defaultRatio: 16, defaultGrindSetting: 48, description: "Pour-over cone brewer." }],
      },
    };
    const body = catalogue[url.pathname];
    if (!body) { response.writeHead(404).end(); return; }
    if (url.pathname === "/api/machines" && (url.searchParams.has("brewMethod") || [...url.searchParams.keys()].some((name) => name !== "brand"))) {
      response.writeHead(400).end("unsupported machine query");
      return;
    }
    toolSmoke.catalogue_calls += 1;
    if (url.pathname === "/api/grinders") toolSmoke.grinder_catalogue_calls += 1;
    if (url.pathname === "/api/machines") toolSmoke.brewer_catalogue_calls += 1;
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify(body));
    return;
  }
  if (request.method !== "POST") { response.writeHead(405).end(); return; }
  const chunks = [];
  for await (const chunk of request) chunks.push(chunk);
  let body;
  try { body = JSON.parse(Buffer.concat(chunks).toString("utf8")); } catch { response.writeHead(400).end(); return; }
  const text = [...(body?.messages ?? [])].reverse().find((message) => message?.role === "user")?.content;
  if (typeof text !== "string") { response.writeHead(400).end(); return; }
  if (body.model === "e2e-invariant-model") {
    const prompt = body.messages.find((message) => message.role === "system")?.content;
    let payload;
    try { payload = JSON.parse(text); } catch { response.writeHead(400).end("invalid invariant payload"); return; }
    const equipmentChecker = typeof prompt === "string" && prompt.includes("ONLY the equipment-availability checker");
    if (equipmentChecker && payload?.subject === "user_input" && typeof payload.text === "string" && payload.text.includes("invariant-equipment-pre-conflict")) {
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ status: "violation", reason: "V60 явно сломан.", repair_instruction: "Назовите доступное оборудование или выберите альтернативный способ." }) } }], usage: { prompt_tokens: 7, completion_tokens: 3 } }));
      return;
    }
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ status: "allow" }) } }], usage: { prompt_tokens: 7, completion_tokens: 3 } }));
    return;
  }
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
    // The extractor returns complete snapshots. Equipment and beans are global by
    // default; negative availability is retained as a replacement fact.
    let facts = [...(payload?.global_facts ?? [])];
    let projectFacts = [...(payload?.project_facts ?? [])];
    if (latest.includes("V60") && !facts.includes("Оборудование: V60")) facts.push("Оборудование: V60");
    if (latest.includes("Эфиоп") && !facts.includes("Есть зёрна: Эфиопия")) facts.push("Есть зёрна: Эфиопия");
    if (latest.includes("Только для этого проекта") && latest.includes("Кения") && !projectFacts.includes("Есть зёрна: Кения")) projectFacts.push("Есть зёрна: Кения");
    if (latest.includes("V60 больше нет") || latest.includes("V60 сломан")) {
      facts = facts.filter((fact) => fact !== "Оборудование: V60");
      if (!facts.includes("Оборудование: V60 — сломано")) facts.push("Оборудование: V60 — сломано");
    }
    if (latest.includes("Эфиопия закончилась")) {
      facts = facts.filter((fact) => fact !== "Есть зёрна: Эфиопия");
      if (!facts.includes("Зёрна: Эфиопия закончились")) facts.push("Зёрна: Эфиопия закончились");
    }
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
  const taskStateRaw = extractTaskState(body.messages.find(message => message.role === "system")?.content);
  const taskState = taskStateRaw ? JSON.parse(taskStateRaw) : null;
  if (Array.isArray(body.tools) && taskState?.stage !== "research_input_data") {
    toolSmoke.unexpected_tools += 1;
    response.writeHead(400).end("tools are allowed only on research_input_data");
    return;
  }
  const noToolResearch = taskState?.stage === "research_input_data" && text.includes("no-tool");
  if (taskState?.stage === "research_input_data" && Array.isArray(body.tools)) {
    if (noToolResearch) {
      toolSmoke.no_tool_requests += 1;
    } else {
      toolSmoke.tool_requests += 1;
      toolSmoke.batch_tool_requests += 1;
      toolSmoke.batch_tool_calls += 2;
      response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({
        choices: [{ message: { role: "assistant", tool_calls: [
          { id: "fixture-brewmark-grinder", type: "function", function: { name: "brewmark_list_grinders", arguments: "{\"brand\":\"Timemore\",\"name\":\"C5 ESP Pro\"}" } },
          { id: "fixture-brewmark-brewer", type: "function", function: { name: "brewmark_list_brewers", arguments: "{\"brand\":\"DeLonghi\",\"name\":\"EC685\"}" } },
        ] } }],
        usage: { prompt_tokens: 50, completion_tokens: 10, total_tokens: 60 },
      }));
      return;
    }
  }
  if (taskState?.stage === "research_input_data") {
    const assistantIndex = body.messages.findIndex(message => message?.role === "assistant" && Array.isArray(message.tool_calls));
    const assistantCalls = assistantIndex >= 0 ? body.messages[assistantIndex].tool_calls : [];
    const toolMessages = assistantIndex >= 0 ? body.messages.slice(assistantIndex + 1, assistantIndex + 1 + assistantCalls.length) : [];
    if (assistantIndex >= 0 || toolMessages.some(message => message?.role === "tool")) {
      const expected = [
        { id: "fixture-brewmark-grinder", name: "brewmark_list_grinders", arguments: { brand: "Timemore", name: "C5 ESP Pro" } },
        { id: "fixture-brewmark-brewer", name: "brewmark_list_brewers", arguments: { brand: "DeLonghi", name: "EC685" } },
      ];
      if (assistantCalls.length !== expected.length || toolMessages.length !== expected.length) {
        toolSmoke.pairing_valid = false;
        toolSmoke.ordered_batch_valid = false;
        response.writeHead(400).end("tool batch was lost");
        return;
      }
      for (const [index, call] of assistantCalls.entries()) {
        let argumentsValue;
        let result;
        try { argumentsValue = JSON.parse(call?.function?.arguments); result = JSON.parse(toolMessages[index]?.content); } catch { toolSmoke.pairing_valid = false; response.writeHead(400).end("tool arguments or result is not JSON"); return; }
        if (call?.id !== expected[index].id || call?.function?.name !== expected[index].name || JSON.stringify(argumentsValue) !== JSON.stringify(expected[index].arguments) || toolMessages[index]?.role !== "tool" || toolMessages[index]?.tool_call_id !== call.id || !Object.hasOwn(result, "content") || !Object.hasOwn(result, "structuredContent") || !Object.hasOwn(result, "isError")) {
          toolSmoke.pairing_valid = false;
          toolSmoke.ordered_batch_valid = false;
          response.writeHead(400).end("tool batch pairing/result was lost");
          return;
        }
      }
      toolSmoke.continuations += 1;
    }
  }
  const key = `${body.model}:${text}`;
  const count = (seen.get(key) ?? 0) + 1;
  seen.set(key, count);
  if ((titleRequest && text.includes("title-slow")) || (!titleRequest && text.startsWith("slow"))) await new Promise((resolve) => setTimeout(resolve, titleRequest ? 4000 : 8000));
  if (!titleRequest && text.includes("auto-slow") && taskState?.stage === "research_input_data" && taskState.prior_outputs?.length === 1) await new Promise((resolve) => setTimeout(resolve, 8000));
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
  if (!titleRequest && taskState && text.includes("resume-invalid") && count === 3) {
    response.writeHead(200, { "Content-Type": "application/json" }).end(JSON.stringify({ choices: [], usage }));
    return;
  }
  let answer = titleRequest ? "Название диалога" : `Ответ: ${text} [${body.model}]`;
  if (!titleRequest && taskState) {
    const task = taskState;
    let stage = task.stage;
    let plan = task.plan;
    let current = task.current_plan_item;
    let status = "active";
    let confirmed = false;
    let positive = false;
    if (task.first) {
      stage = "clarify_input"; plan = []; current = "";
    } else if (stage === "clarify_input" && /кофемолк|niche/i.test(text)) {
      stage = "research_input_data"; confirmed = true;
      plan = [{ id: "grinder", title: "Узнать информацию о кофемолке", status: "current" }, { id: "recipe", title: "Подобрать стартовый рецепт", status: "pending" }]; current = "grinder";
    } else if (stage === "research_input_data" && (!text.includes("same-stage") || task.prior_outputs.length >= 2)) {
      stage = "execution"; plan = plan.map(item => ({ ...item, status: item.id === "recipe" ? "current" : "completed" })); current = "recipe";
    } else if (stage === "execution") {
      stage = "user_feedback"; plan = plan.map(item => ({ ...item, status: "completed" })); current = "";
    } else if (stage === "user_feedback") {
      positive = /^(да|спасибо|подходит|готово)$/i.test(text);
      if (positive) status = "done";
      else { stage = "execution"; current = `revision-${step}`; plan = [...plan, { id: current, title: "Скорректировать рецепт по отзыву", status: "current" }]; }
    }
    const clarify = stage === "clarify_input";
    const expected = status === "done" ? "none" : clarify ? "user: Укажите модель кофемолки или подтвердите цель" : stage === "user_feedback" ? "user: Оцените рецепт" : "agent: Выполнить сохранённый шаг";
    if (task.stage === "research_input_data") {
      const source = noToolResearch ? "no-tool" : "tool";
      answer = `Ответ: ${text} [${body.model}] · research-facts:${source}: оборудование уточнено`;
      toolSmoke.research_proposals += 1;
      if (/рецепт|recipe|18 г|200 мл/i.test(answer) || plan.some(item => item.id === "recipe" && item.status === "completed")) toolSmoke.research_recipe_leaks += 1;
    } else if (task.stage === "execution") {
      const researchOutput = [...(task.prior_outputs ?? [])].reverse().find(output => output.includes("research-facts:"));
      if (researchOutput) {
        toolSmoke.execution_after_research += 1;
        if (researchOutput.includes("research-facts:tool:")) toolSmoke.execution_after_tool += 1;
        if (researchOutput.includes("research-facts:no-tool:")) toolSmoke.execution_after_no_tool += 1;
      }
      answer = `Ответ: ${text} [${body.model}] · Стартовый рецепт: 18 г кофе, 36 г напитка`;
    }
    answer = JSON.stringify({ output: answer, understanding: "Подобрать эспрессо", questions: clarify ? ["Какая кофемолка?"] : [], stage, current_step: clarify ? "Подтвердить цель и модель кофемолки" : task.current_step || "Проверить параметры рецепта", expected_action: expected, status, plan, current_plan_item: current, goal_confirmed: confirmed, positive_feedback: positive });
  }
  response.writeHead(200, { "Content-Type": "application/json" });
  response.end(JSON.stringify({ choices: [{ message: { role: "assistant", content: answer } }], usage }));
}).listen(port, process.env.BARISTA_PROVIDER_HOST ?? "127.0.0.1", () => console.log(`fixture provider on ${port}`));

function extractTaskState(prompt) {
  if (typeof prompt !== "string") return null;

  const start = prompt.indexOf("TASK_STATE:");
  if (start === -1) return null;
  const jsonStart = prompt.indexOf("{", start + "TASK_STATE:".length);
  if (jsonStart === -1) return null;

  let depth = 0;
  let inString = false;
  let escaped = false;
  for (let index = jsonStart; index < prompt.length; index += 1) {
    const character = prompt[index];
    if (inString) {
      if (escaped) escaped = false;
      else if (character === "\\") escaped = true;
      else if (character === '"') inString = false;
      continue;
    }
    if (character === '"') inString = true;
    else if (character === "{") depth += 1;
    else if (character === "}" && --depth === 0) return prompt.slice(jsonStart, index + 1);
  }
  return null;
}
