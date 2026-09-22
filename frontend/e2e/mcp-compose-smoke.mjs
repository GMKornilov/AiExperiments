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

const result = await call("tools/call", { name: "brewmark_list_grinders", arguments: {} });
if (result?.isError || !result?.structuredContent || typeof result.structuredContent !== "object") throw new Error("Grinders tool failed");
const catalogue = result.structuredContent;
if (!Array.isArray(catalogue.grinders) || catalogue.grinders.length !== 1 || catalogue.grinders[0]?.model !== "C40 MK4") {
  throw new Error("Grinders tool returned an unexpected fixture");
}
console.log("MCP JSON-RPC smoke: 4 tools and grinders catalogue verified");
