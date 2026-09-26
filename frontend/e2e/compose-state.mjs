// Run against an isolated Compose project, before and after service recreation.
import { readFile, writeFile } from "node:fs/promises";
import assert from "node:assert/strict";
const [mode, file] = process.argv.slice(2);
const base = process.env.BARISTA_E2E_URL ?? "http://localhost:13030";
let cookie = "";
async function call(path, body, method = body === undefined ? "GET" : "POST") {
  const response = await fetch(`${base}${path}`, { method, headers: { "content-type": "application/json", ...(cookie ? { cookie } : {}) }, ...(body === undefined ? {} : { body: JSON.stringify(body) }) });
  cookie = response.headers.get("set-cookie")?.split(";")[0] ?? cookie;
  assert.ok(response.ok, `${method} ${path}: ${response.status} ${await response.clone().text()}`);
  return response.status === 204 ? null : response.json();
}
function assertAutonomousStepOutputs(messages, input, researchSource) {
  const inputIndex = messages.findLastIndex(message => message.role === "user" && message.text === input);
  assert.notEqual(inputIndex, -1, "the initiating task input must be persisted");
  const outputs = messages.slice(inputIndex + 1);
  assert.equal(outputs.length, 3, "only three accepted user-facing task outputs may create bubbles");
  assert.deepEqual(outputs.map(message => message.role), ["assistant", "assistant", "assistant"], "tool-loop and internal calls must not create chat bubbles");
  assert.deepEqual(outputs.map(message => message.text), [
    `Ответ: ${input} [e2e-fixture-model]`,
    `Ответ: ${input} [e2e-fixture-model] · research-facts:${researchSource}: оборудование уточнено`,
    `Ответ: ${input} [e2e-fixture-model] · Стартовый рецепт: 18 г кофе, 36 г напитка`,
  ], "accepted autonomous outputs must remain separate and ordered");
}
if (mode === "prepare") {
  const projects = await call("/api/projects", { title: "Smoke project" });
  const pid = projects.selected_project_id;
  const chat = await call(`/api/projects/${pid}/chats`, {});
  const cid = chat.id;
  const root = `/api/projects/${pid}/chats/${cid}`;
  await call("/api/profiles", { name: "Smoke profile", style: "short", constraints: "safe", additional_context: "home" });
  const normal = await call(`${root}/messages`, { client_message_id: "normal", text: "У меня есть V60 и зёрна Эфиопия" });
  assert.equal(normal.messages.length, 2);
  const task = await call(`${root}/tasks/input`, { client_message_id: "task-one", text: "Подобрать эспрессо" });
  const tid = task.chat.tasks[0].id;
  const second = await call(`${root}/tasks/input`, { client_message_id: "task-two", candidate_task_id: tid, text: "Кофемолка Timemore C5 ESP Pro и кофемашина DeLonghi EC685" });
  assert.equal(second.chat.tasks[0].stage, "user_feedback");
  assertAutonomousStepOutputs(second.chat.messages, "Кофемолка Timemore C5 ESP Pro и кофемашина DeLonghi EC685", "tool");
  assert.deepEqual((await call(root)).messages, second.chat.messages, "chat read must return the same separate task outputs as the successful BFF response");
  const noToolChat = await call(`/api/projects/${pid}/chats`, {});
  const noToolRoot = `/api/projects/${pid}/chats/${noToolChat.id}`;
  await call(`${noToolRoot}/tasks/input`, { client_message_id: "no-tool-first", text: "Подобрать эспрессо no-tool" });
  const noTool = await call(`${noToolRoot}/tasks/input`, { client_message_id: "no-tool-second", text: "no-tool Кофемолка уже известна, все данные об оборудовании уже известны" });
  assert.equal(noTool.chat.tasks[0].stage, "user_feedback");
  assertAutonomousStepOutputs(noTool.chat.messages, "no-tool Кофемолка уже известна, все данные об оборудовании уже известны", "no-tool");
  await call(`${root}/tasks/${tid}/pause`, {});
  // Memory failure in ordinary chat retains a single pair and old facts.
  const failedMemory = await call(`${root}/messages`, { client_message_id: "normal-memory-error", text: "memory-error" });
  assert.equal(failedMemory.memory_status, "error");
  assert.equal(failedMemory.messages.length, 10, "memory extractor failure must retain the three separate accepted task outputs and add only the ordinary user/assistant pair");
  const facts = await call(`/api/projects/${pid}/memory`);
  assert.deepEqual(facts.global_facts, ["Оборудование: V60", "Есть зёрна: Эфиопия"]);
  assert.deepEqual(facts.project_facts, []);
  const logs = await call(`/api/admin/logs?dialog_id=${cid}&action=lookup`);
  assert.equal(logs.found, true);
  assert.ok(logs.logs.some(log => log.purpose === "task_step"));
  assert.ok(logs.logs.every(log => !log.payload && !log.text));
  const finalChat = await call(`/api/projects/${pid}/chats`, {});
  const finalRoot = `/api/projects/${pid}/chats/${finalChat.id}`;
  let finalTask;
  for (const [index, text] of ["Подобрать эспрессо", "Кофемолка Timemore C5 ESP Pro и кофемашина DeLonghi EC685", "эспрессо следующий шаг", "эспрессо готовый результат"].entries()) {
    finalTask = await call(`${finalRoot}/tasks/input`, { client_message_id: `final-${index}`, text });
  }
  const finalID = finalTask.chat.tasks[0].id;
  await call(`${finalRoot}/tasks/${finalID}/pause`, {});
  const completed = await call(`${finalRoot}/tasks/${finalID}/resume`, { client_message_id: "final-resume", text: "спасибо" });
  assert.equal(completed.chat.tasks[0].status, "done");
  const finalReplay = await call(`${finalRoot}/tasks/${finalID}/resume`, { client_message_id: "final-resume", text: "Продолжить сохранённый шаг." });
  assert.deepEqual(finalReplay, completed);
  const saved = await call(root);
  const finalFacts = await call(`/api/projects/${pid}/memory`);
  assert.deepEqual(finalFacts.global_facts, facts.global_facts);
  assert.deepEqual(finalFacts.project_facts, facts.project_facts);
  await writeFile(file, JSON.stringify({ cookie, pid, cid, tid, saved, facts: finalFacts }));
  console.log("PASS: normal chat, extractor error, profile, research paths, task plan, pause, admin, redaction");
  console.log("PASS: lost final Resume response replays completed task without duplicate");
} else if (mode === "verify") {
  const expected = JSON.parse(await readFile(file, "utf8"));
  cookie = expected.cookie;
  const root = `/api/projects/${expected.pid}/chats/${expected.cid}`;
  assert.deepEqual(await call(root), expected.saved);
  assert.deepEqual(await call(`/api/projects/${expected.pid}/memory`), expected.facts);
  const profiles = await call("/api/profiles");
  assert.equal(profiles.profiles.find(profile => profile.id === profiles.active_profile_id).name, "Smoke profile");
  const replay = await call(`${root}/tasks/input`, { client_message_id: "task-two", candidate_task_id: expected.tid, text: "Кофемолка Timemore C5 ESP Pro и кофемашина DeLonghi EC685" });
  assert.equal(replay.chat.messages.length, expected.saved.messages.length);
  const otherCookie = cookie; cookie = "";
  assert.equal((await call("/api/projects")).projects.length, 0);
  cookie = otherCookie;
  console.log("PASS: volume recreation, exact confirmed state, profiles, operation replay, cookie isolation");
} else if (mode === "tool-audit") {
  const state = JSON.parse(await readFile(file, "utf8"));
  assert.ok(state.tool_requests >= 1, "research must expose tools");
  assert.ok(state.batch_tool_requests >= 1, "research must request an MCP tool batch");
  assert.equal(state.batch_tool_calls, state.batch_tool_requests * 2, "each research batch must contain grinder and brewer lookups");
  assert.ok(state.no_tool_requests >= 1, "research must permit skipping MCP when equipment data are sufficient");
  assert.equal(state.continuations, state.tool_requests, "each assistant tool_call must have exactly one paired continuation");
  assert.equal(state.pairing_valid, true, "continuation must retain tool_call_id and complete result envelope");
  assert.equal(state.ordered_batch_valid, true, "batch calls and tool messages must preserve deterministic order");
  assert.ok(state.grinder_catalogue_calls >= state.batch_tool_requests, "MCP must call the grinder catalogue for each batch");
  assert.ok(state.brewer_catalogue_calls >= state.batch_tool_requests, "MCP must call the brewer catalogue for each batch");
  assert.ok(state.catalogue_calls >= state.batch_tool_calls, "MCP must call both BrewMark catalogues for each batch");
  assert.equal(state.unexpected_tools, 0, "tools must not be exposed outside research_input_data");
  assert.equal(state.research_recipe_leaks, 0, "research must not write a recipe or complete its plan item");
  assert.ok(state.research_proposals >= 2, "both research paths must produce equipment-only proposals");
  assert.ok(state.execution_after_tool >= 1, "tool research must be followed by a distinct execution call");
  assert.ok(state.execution_after_no_tool >= 1, "no-tool research must be followed by a distinct execution call");
  assert.equal(state.execution_after_research, state.execution_after_tool + state.execution_after_no_tool);
  console.log("PASS: research tool/no-tool paths, full tool result, equipment-only proposals, separate recipe execution call");
} else if (mode === "interrupt") {
  const expected = JSON.parse(await readFile(file, "utf8"));
  cookie = expected.cookie;
  const chat = await call(`/api/projects/${expected.pid}/chats`, {});
  const root = `/api/projects/${expected.pid}/chats/${chat.id}`;
  void call(`${root}/tasks/input`, { client_message_id: "interrupted", text: "slow эспрессо restart-fixture" }).catch(() => {});
  let pending;
  for (let attempt = 0; attempt < 50; attempt++) {
    pending = await call(root);
    if (pending.memory_status === "updating" && pending.tasks.length) break;
    await new Promise(resolve => setTimeout(resolve, 20));
  }
  assert.equal(pending.memory_status, "updating");
  assert.equal(pending.messages.length, 0);
  await writeFile(`${file}.interrupted`, JSON.stringify({ cookie, root, tid: pending.tasks[0].id, cid: chat.id }));
  console.log("READY: durable in-flight marker; kill the isolated backend now");
  process.exit(0);
} else if (mode === "verify-interrupted") {
  const expected = JSON.parse(await readFile(`${file}.interrupted`, "utf8"));
  cookie = expected.cookie;
  const restored = await call(expected.root);
  assert.equal(restored.tasks[0].status, "paused");
  assert.equal(restored.tasks[0].stage, "clarify_input");
  assert.equal(restored.messages.length, 0);
  const resumed = await call(`${expected.root}/tasks/${expected.tid}/resume`, { client_message_id: "resume-after-kill", text: "Продолжить сохранённый шаг." });
  assert.equal(resumed.chat.messages.length, 2);
  assert.equal(resumed.chat.tasks[0].stage, "clarify_input");
  const logs = await call(`/api/admin/logs?dialog_id=${expected.cid}&action=lookup`);
  const completedTitles = logs.logs.filter(log => log.purpose === "title" && log.event === "llm_response" && log.result === "success" && typeof log.call_id === "string" && log.call_id);
  assert.equal(completedTitles.length, 1);
  const titleCallID = completedTitles[0].call_id;
  assert.equal(new Set(completedTitles.map(log => log.call_id)).size, 1);
  assert.equal(logs.logs.filter(log => log.call_id === titleCallID && log.event === "llm_request" && log.purpose === "title" && log.result === "started").length, 1);
  console.log("PASS: killed in-flight process restores paused step; Resume commits once; one completed post-Resume title call");
} else if (mode === "audit") {
  const config = JSON.parse(await readFile(`${file}/compose.json`, "utf8"));
  assert.equal(config.services["barista-api"].ports, undefined);
  assert.equal(config.services["barista-web"].depends_on["barista-api"].condition, "service_healthy");
  assert.ok(config.services["barista-api"].volumes.some(volume => volume.type === "volume" && volume.target === "/app/data"));
  const logs = await readFile(`${file}/runtime.log`, "utf8");
  const state = await readFile(`${file}/state.json`, "utf8");
  const imageEnv = await readFile(`${file}/image-env.json`, "utf8");
  for (const marker of ["e2e-dummy-credential", "Кофемолка Niche Zero", "ДлинныйТекст", "У меня есть V60", "Оборудование: V60", "Есть зёрна: Эфиопия", "Smoke profile"]) assert.ok(!logs.includes(marker), `runtime payload leak: ${marker}`);
  for (const marker of ["e2e-dummy-credential", "SECURE_API_KEY", "BARISTA_SMOKE_SENTINEL"]) {
    assert.ok(!state.includes(marker), `state credential leak: ${marker}`);
    assert.ok(!imageEnv.includes(marker), `image credential leak: ${marker}`);
  }
  assert.ok(logs.includes('"correlation_id"'));
  console.log("PASS: private API, health dependency, named volume, runtime-only env, logs/state/image redaction");
} else throw new Error("Usage: node compose-state.mjs prepare|verify|tool-audit|interrupt|verify-interrupted /tmp/expected.json OR audit /tmp/fixture-dir");
