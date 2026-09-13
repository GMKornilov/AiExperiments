#!/usr/bin/env node
// Runs the documented 3 × 4 × 12 comparison against an already authorized
// stack. It neither reads configuration nor sends credentials to a provider.

import { mkdir, writeFile, access } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { dirname, resolve } from "node:path";

const scenario = [
  "Готовлю эспрессо. Молоко и сахар нельзя. Цель — более сладкий и сбалансированный вкус.",
  "Рецепт: доза 18 г, выход 36 г, температура 93°C, помол 12. Не меняй рецепт без моего согласия.",
  "Предпочитаю менять за раз только одну переменную. Это договорённость.",
  "Исправление: доза теперь 17 г, а не 18 г. Выход остаётся 36 г.",
  "Кофе горчит. Объясни причины, но рецепт пока не меняй.",
  "Согласен на один тест: помол чуть крупнее, до 12,5. Остальное не меняй.",
  "Температуру 93°C тоже не меняем, даже если предложишь другой вариант.",
  "Как оценить результат этого единственного теста по вкусу и времени пролива?",
  "Если тест не поможет, следующим шагом можно менять только выход; 17 г сохраняем.",
  "Повтори цель, запреты, договорённость и актуальные параметры. Назови исправление.",
  "Предложи ровно одно следующее действие при сохранении ограничений.",
  "Можно ли добавить молоко, вернуть 18 г или изменить температуру? Объясни кратко.",
];
const strategies = ["sliding_window", "facts", "branching", "summary"];

function parseArgs(argv) {
  const values = new Map();
  for (let index = 0; index < argv.length; index += 2) {
    if (!argv[index].startsWith("--") || !argv[index + 1]) throw new Error("usage: node e2e/context-compare.mjs --base-url URL --output PATH");
    values.set(argv[index].slice(2), argv[index + 1]);
  }
  if (!values.get("base-url") || !values.get("output")) throw new Error("usage: node e2e/context-compare.mjs --base-url URL --output PATH");
  return { baseURL: values.get("base-url").replace(/\/$/, ""), output: resolve(values.get("output")) };
}

function answerAfter(dialog, userText) {
  const messages = Array.isArray(dialog.messages) ? dialog.messages : [];
  const index = messages.findIndex((message) => message?.role === "user" && message.text === userText);
  const answer = index >= 0 ? messages[index + 1] : undefined;
  return answer?.role === "assistant" && typeof answer.text === "string" ? answer.text : null;
}

function userAfter(dialog, userText) {
  const messages = Array.isArray(dialog.messages) ? dialog.messages : [];
  return messages.find((message) => message?.role === "user" && message.text === userText) ?? null;
}

class API {
  constructor(baseURL) {
    this.baseURL = baseURL;
    // Explicitly keep one browser session across every dialog run through the BFF.
    this.session = randomUUID();
  }

  async call(method, path, body) {
    const response = await fetch(`${this.baseURL}${path}`, {
      method,
      headers: { "Content-Type": "application/json", Cookie: `barista_session=${this.session}` },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(90_000),
    });
    const json = await response.json().catch(() => null);
    if (!response.ok || !json || typeof json !== "object") {
      throw new Error(`${method} ${path}: HTTP ${response.status}; category=${json?.error?.category ?? "unknown"}`);
    }
    return json;
  }
}

function stepCapture(dialog, number, input) {
  const user = userAfter(dialog, input);
  const compression = dialog.compression ?? {};
  return {
    number,
    input,
    user_status: user?.status ?? null,
    error_category: user?.error_category ?? null,
    answer: answerAfter(dialog, input),
    accounted_tokens: dialog.accounted_tokens,
    facts_tokens: dialog.facts_tokens,
    facts_usage_missing: dialog.facts_usage_missing,
    summary_tokens: compression.summary_tokens,
    summary_usage_missing: compression.summary_usage_missing,
  };
}

async function run(api, result, persist) {
  const { strategy, repetition } = result;
  let dialog = await api.call("POST", "/api/dialogs", { context_strategy: strategy });
  if (typeof dialog.id !== "string" || !dialog.id) throw new Error("dialog creation did not return id");
  const dialogID = dialog.id;
  for (const [index, text] of scenario.entries()) {
    dialog = await api.call("POST", `/api/dialogs/${encodeURIComponent(dialogID)}/messages`, {
      client_message_id: `context-compare-${strategy}-${repetition}-${index + 1}`,
      text,
    });
    const step = stepCapture(dialog, index + 1, text);
    result.steps.push(step);
    result.dialog_id = dialogID;
    result.final_dialog = dialog;
    await persist();
    if (step.user_status !== "success" || step.answer === null) {
      throw new Error(`${strategy} run ${repetition}, message ${index + 1}: user_status=${step.user_status}; category=${step.error_category ?? "unknown"}`);
    }
  }
  const compression = dialog.compression ?? {};
  Object.assign(result, {
    completed: true,
    facts: dialog.facts,
    accounted_tokens: dialog.accounted_tokens,
    facts_usage: dialog.facts_tokens,
    facts_usage_missing: dialog.facts_usage_missing,
    summary_usage: compression.summary_tokens,
    summary_usage_missing: compression.summary_usage_missing,
  });
  await persist();
}

async function main() {
  const { baseURL, output } = parseArgs(process.argv.slice(2));
  try { await access(output); throw new Error(`refusing to overwrite existing capture: ${output}`); } catch (error) { if (error?.code !== "ENOENT") throw error; }
  const api = new API(baseURL);
  const capture = { created_at: new Date().toISOString(), scenario, runs: [] };
  const persist = async () => {
    await mkdir(dirname(output), { recursive: true });
    await writeFile(output, `${JSON.stringify(capture, null, 2)}\n`);
  };
  await persist();
  for (const strategy of strategies) {
    for (let repetition = 1; repetition <= 3; repetition += 1) {
      process.stdout.write(`${strategy}: прогон ${repetition}/3\n`);
      const result = { strategy, repetition, completed: false, steps: [] };
      capture.runs.push(result);
      await persist();
      await run(api, result, persist);
    }
  }
  await persist();
  process.stdout.write(`Сохранено 12 прогонов: ${output}\n`);
}

await main();
