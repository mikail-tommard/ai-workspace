// The SDK sees normal Fetch Request/Response objects. Only TCP is replaced by
// an IPC roundtrip into the production Go HTTP handler. No MCP responses are mocked.
import { spawn } from "node:child_process";
import { resolve } from "node:path";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";
const process = spawn(resolve(".cache/httpbridge"), [], {
  stdio: ["pipe", "pipe", "inherit"],
});
let id = 0,
  buffer = "";
const pending = new Map();
process.stdout.on("data", (chunk) => {
  buffer += chunk;
  let end;
  while ((end = buffer.indexOf("\n")) >= 0) {
    const line = buffer.slice(0, end);
    buffer = buffer.slice(end + 1);
    const r = JSON.parse(line),
      resolve = pending.get(r.id);
    pending.delete(r.id);
    const headers = new Headers();
    for (const [k, vs] of Object.entries(r.headers))
      for (const v of vs) headers.append(k, v);
    resolve(
      new Response(
        [204, 205, 304].includes(r.status)
          ? null
          : r.bodyBase64
            ? Buffer.from(r.bodyBase64, "base64")
            : r.body,
        {
          status: r.status,
          headers,
        },
      ),
    );
  }
});
const fetchBridge = async (url, init = {}) => {
  const req = new Request(url, init);
  const key = ++id;
  return new Promise(async (resolve, reject) => {
    pending.set(key, resolve);
    try {
      process.stdin.write(
        JSON.stringify({
          id: key,
          method: req.method,
          url: req.url,
          headers: Object.fromEntries(req.headers),
          bodyBase64:
            req.method === "GET" || req.method === "HEAD"
              ? ""
              : Buffer.from(await req.arrayBuffer()).toString("base64"),
        }) + "\n",
      );
    } catch (e) {
      pending.delete(key);
      reject(e);
    }
  });
};
const client = new Client({
  name: "official-sdk-http-independent",
  version: "1",
});
try {
  const transport = new StreamableHTTPClientTransport(
    new URL("http://127.0.0.1:8080/mcp"),
    {
      fetch: fetchBridge,
      requestInit: {
        headers: {
          Authorization: "Bearer independent-http-test-token-0123456789",
        },
      },
    },
  );
  await client.connect(transport);
  assert.ok(transport.sessionId);
  const tools = await client.listTools();
  assert.equal(tools.tools.length, 28);
  const result = await client.callTool({
    name: "math_create_problem",
    arguments: { kind: "linear" },
  });
  assert.equal(result.isError, false);
  assert.equal(result.structuredContent.widget.type, "math_problem");
  const photo = await readFile(resolve("e2e/fixtures/photo.png"));
  const form = new FormData();
  form.append("file", new Blob([photo], { type: "image/png" }), "photo.png");
  const uploaded = await fetchBridge(
    "http://127.0.0.1:8080/api/media/default",
    {
      method: "POST",
      headers: {
        Authorization: "Bearer independent-http-test-token-0123456789",
        "X-Workspace-Request": "1",
      },
      body: form,
    },
  );
  assert.equal(uploaded.status, 201);
  const media = await uploaded.json();
  assert.equal(media.widget.type, "image");
  const served = await fetchBridge("http://127.0.0.1:8080" + media.asset.url, {
    headers: { Authorization: "Bearer independent-http-test-token-0123456789" },
  });
  assert.equal(served.status, 200);
  assert.deepEqual(Buffer.from(await served.arrayBuffer()), photo);
  const invalid = await client.callTool({
    name: "math_create_problem",
    arguments: { unknown: true },
  });
  assert.equal(invalid.isError, true);
  const state = await client.callTool({
    name: "workspace_get_state",
    arguments: {},
  });
  assert.equal(state.structuredContent.state.widgets.length, 2);
  assert.ok(state.structuredContent.state.widgets.some(w=>w.type==="image"));
  await transport.terminateSession();
  console.log(
    JSON.stringify(
      {
        ok: true,
        transport:
          "official SDK StreamableHTTP over IPC Fetch bridge into actual Go handler",
        tools: tools.tools.length,
        checks: [
          "initialize",
          "session header",
          "initialized",
          "tools/list",
          "real mutation",
          "multipart upload / authenticated binary media serving",
          "strict validation",
          "state read",
          "DELETE session",
        ],
      },
      null,
      2,
    ),
  );
} finally {
  await client.close();
  process.stdin.end();
  process.kill("SIGTERM");
}
