import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

const endpoint = process.argv[2];
if (!endpoint) throw new Error("MCP endpoint is required");

const client = new Client({ name: "brewmark-compose-smoke", version: "1.0.0" });
try {
  await client.connect(new StreamableHTTPClientTransport(new URL(endpoint)));
  const tools = await client.listTools();
  if (tools.tools.length !== 4) throw new Error(`Expected 4 tools, received ${tools.tools.length}`);
  const expected = [
    "brewmark_list_brew_methods",
    "brewmark_list_brewers",
    "brewmark_list_filters",
    "brewmark_list_grinders",
  ];
  if (tools.tools.map((tool) => tool.name).join(",") !== expected.join(",")) throw new Error("Unexpected MCP tool registry");

  const result = await client.callTool({ name: "brewmark_list_grinders", arguments: {} });
  if (result.isError || !result.structuredContent || typeof result.structuredContent !== "object") throw new Error("Grinders tool failed");
  const catalogue = result.structuredContent;
  if (!Array.isArray(catalogue.grinders) || catalogue.grinders.length !== 1 || catalogue.grinders[0]?.model !== "C40 MK4") {
    throw new Error("Grinders tool returned an unexpected fixture");
  }
  console.log("MCP SDK smoke: 4 tools and grinders catalogue verified");
} finally {
  await client.close();
}
