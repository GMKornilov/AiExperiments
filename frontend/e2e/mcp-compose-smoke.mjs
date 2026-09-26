const endpoint = process.argv[2];
if (!endpoint) throw new Error("MCP endpoint is required");

let nextID = 1;

async function call(method, params) {
  const response = await fetch(endpoint, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ jsonrpc: "2.0", id: nextID++, method, params }),
  });
  if (!response.ok) throw new Error(`MCP ${method} returned HTTP ${response.status}`);
  const payload = await response.json();
  if (payload.error) throw new Error(`MCP ${method} returned a protocol error`);
  return payload.result;
}

await call("initialize", {
  protocolVersion: "2025-11-25",
  capabilities: {},
  clientInfo: { name: "brewmark-compose-smoke", version: "1.0.0" },
});
const tools = await call("tools/list", {});
if (!Array.isArray(tools?.tools) || tools.tools.length !== 4) throw new Error(`Expected 4 tools, received ${tools?.tools?.length ?? 0}`);
const expected = [
  "brewmark_list_brew_methods",
  "brewmark_list_brewers",
  "brewmark_list_filters",
  "brewmark_list_grinders",
];
if (tools.tools.map((tool) => tool.name).join(",") !== expected.join(",")) throw new Error("Unexpected MCP tool registry");

const result = await call("tools/call", {
  name: "brewmark_list_grinders",
  arguments: { brand: "Timemore", name: "C5 ESP Pro" },
});
if (result?.isError || !result?.structuredContent || typeof result.structuredContent !== "object") throw new Error("Grinders tool failed");
const catalogue = result.structuredContent;
const grinder = catalogue.grinders?.[0];
if (!Array.isArray(catalogue.grinders) || catalogue.grinders.length !== 1 || catalogue.matchStatus !== "exact" || grinder?.name !== "C5 ESP Pro" || grinder?.settingUnit !== "CLICKS" || grinder?.mokaAnchor !== 20 || grinder?.frenchPressAnchor !== null || grinder?.burrType !== "CONICAL" || !grinder?.createdAt) {
  throw new Error("Grinders tool returned an unexpected fixture");
}

const brewerResult = await call("tools/call", {
  name: "brewmark_list_brewers",
  arguments: { brand: "DeLonghi", name: "EC685" },
});
const brewer = brewerResult?.structuredContent?.brewers?.[0];
if (brewerResult?.isError || brewerResult?.structuredContent?.matchStatus !== "exact" || brewer?.name !== "EC685" || brewer?.minBatchGrams !== 7 || brewer?.maxBatchGrams !== 18 || !brewer?.createdAt) {
  throw new Error("Brewers tool returned an unexpected fixture");
}

const filtersResult = await call("tools/call", { name: "brewmark_list_filters", arguments: {} });
const filter = filtersResult?.structuredContent?.filters?.[0];
if (filtersResult?.isError || filter?.name !== "V60 Paper Filter 02" || filter?.grindAdjustment !== 1 || !filter?.createdAt) {
  throw new Error("Filters tool returned an unexpected fixture");
}

const methodsResult = await call("tools/call", { name: "brewmark_list_brew_methods", arguments: {} });
const method = methodsResult?.structuredContent?.methods?.[0];
if (methodsResult?.isError || method?.id !== "V60" || method?.defaultRatio !== 16 || method?.defaultGrindSetting !== 48 || method?.description !== "Pour-over cone brewer.") {
  throw new Error("Brew methods tool returned an unexpected fixture");
}
console.log("MCP JSON-RPC smoke: 4 tools and all catalogue schemas verified");
