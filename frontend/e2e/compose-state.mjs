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
  const second = await call(`${root}/tasks/input`, { client_message_id: "task-two", candidate_task_id: tid, text: "Кофемолка Niche Zero" });
  assert.equal(second.chat.tasks[0].stage, "user_feedback");
  await call(`${root}/tasks/${tid}/pause`, {});
  // Memory failure in ordinary chat retains a single pair and old facts.
  const failedMemory = await call(`${root}/messages`, { client_message_id: "normal-memory-error", text: "memory-error" });
  assert.equal(failedMemory.memory_status, "error");
  assert.equal(failedMemory.messages.length, 8);
  const facts = await call(`/api/projects/${pid}/memory`);
  assert.deepEqual(facts.global_facts, ["Оборудование: V60"]);
  assert.deepEqual(facts.project_facts, ["Есть зёрна: Эфиопия"]);
  const logs = await call(`/api/admin/logs?dialog_id=${cid}&action=lookup`);
  assert.equal(logs.found, true);
  assert.ok(logs.logs.some(log => log.purpose === "task_step"));
  assert.ok(logs.logs.every(log => !log.payload && !log.text));
  const finalChat = await call(`/api/projects/${pid}/chats`, {});
  const finalRoot = `/api/projects/${pid}/chats/${finalChat.id}`;
  let finalTask;
  for (const [index, text] of ["Подобрать эспрессо", "Кофемолка Niche Zero", "эспрессо следующий шаг", "эспрессо готовый результат"].entries()) {
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
  console.log("PASS: normal chat, extractor error, profile, task plan, pause, admin, redaction");
  console.log("PASS: lost final Resume response replays completed task without duplicate");
} else if (mode === "verify") {
  const expected = JSON.parse(await readFile(file, "utf8"));
  cookie = expected.cookie;
  const root = `/api/projects/${expected.pid}/chats/${expected.cid}`;
  assert.deepEqual(await call(root), expected.saved);
  assert.deepEqual(await call(`/api/projects/${expected.pid}/memory`), expected.facts);
  const profiles = await call("/api/profiles");
  assert.equal(profiles.profiles.find(profile => profile.id === profiles.active_profile_id).name, "Smoke profile");
  const replay = await call(`${root}/tasks/input`, { client_message_id: "task-two", candidate_task_id: expected.tid, text: "Кофемолка Niche Zero" });
  assert.equal(replay.chat.messages.length, expected.saved.messages.length);
  const otherCookie = cookie; cookie = "";
  assert.equal((await call("/api/projects")).projects.length, 0);
  cookie = otherCookie;
  console.log("PASS: volume recreation, exact confirmed state, profiles, operation replay, cookie isolation");
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
  assert.ok(logs.logs.every(log => log.purpose !== "title"));
  console.log("PASS: killed in-flight process restores paused step; Resume commits once; no repeated title");
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
} else throw new Error("Usage: node compose-state.mjs prepare|verify|interrupt|verify-interrupted /tmp/expected.json OR audit /tmp/fixture-dir");
