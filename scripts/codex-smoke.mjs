// Uses the installed Codex app-server protocol for MCP discovery, without an LLM turn.
import { spawn } from "node:child_process";
import { mkdtemp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import assert from "node:assert/strict";
const data = await mkdtemp(join(tmpdir(), "forma-codex-"));
const binary = resolve("./ai-workspace");
const config = `mcp_servers={ai_workspace={command=${JSON.stringify(binary)},args=["--transport","stdio","--dashboard=false","--data-dir",${JSON.stringify(data)}],startup_timeout_sec=10}}`;
const proc = spawn(
  "codex",
  [
    "app-server",
    "-c",
    config,
    "-c",
    "analytics.enabled=false",
    "-c",
    "feedback.enabled=false",
    "-c",
    `sqlite_home=${JSON.stringify(data)}`,
    "-c",
    `log_dir=${JSON.stringify(data)}`,
  ],
  { stdio: ["pipe", "pipe", "pipe"] },
);
let next = 1,
  buffer = "",
  stderr = "";
const pending = new Map();
proc.stderr.on("data", (chunk) => (stderr += chunk));
proc.stdout.on("data", (chunk) => {
  buffer += chunk;
  let end;
  while ((end = buffer.indexOf("\n")) >= 0) {
    const line = buffer.slice(0, end);
    buffer = buffer.slice(end + 1);
    try {
      const r = JSON.parse(line);
      if (r.id !== undefined && pending.has(r.id)) {
        const { resolve, reject, timer } = pending.get(r.id);
        clearTimeout(timer);
        pending.delete(r.id);
        r.error
          ? reject(new Error(JSON.stringify(r.error)))
          : resolve(r.result);
      }
    } catch {
      /* Ignore unrelated startup diagnostics. */
    }
  }
});
proc.on("exit", (code) => {
  for (const { reject, timer } of pending.values()) {
    clearTimeout(timer);
    reject(new Error(`Codex exited: ${code}. ${stderr.slice(-1000)}`));
  }
  pending.clear();
});
function request(method, params) {
  const id = next++;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`Timeout: ${method}. ${stderr.slice(-1000)}`));
    }, 25000);
    pending.set(id, { resolve, reject, timer });
    proc.stdin.write(
      JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n",
    );
  });
}
try {
  const init = await request("initialize", {
    clientInfo: { name: "forma-compatibility-check", version: "1.0.0" },
    capabilities: { experimentalApi: true },
  });
  proc.stdin.write(
    JSON.stringify({ jsonrpc: "2.0", method: "initialized" }) + "\n",
  );
  const status = await request("mcpServerStatus/list", {
    serverName: "ai_workspace",
    detail: "toolsAndAuthOnly",
  });
  const servers = status.data || status.servers || [];
  const match = servers.find((s) => s.name === "ai_workspace");
  assert.ok(match, JSON.stringify(status));
  assert.equal(
    Object.keys(match.tools || {}).length,
    28,
    JSON.stringify(match),
  );
  console.log(
    JSON.stringify(
      {
        ok: true,
        codex: init.userAgent || init.serverInfo || "installed CLI",
        server: match.name,
        tools: Object.keys(match.tools).length,
      },
      null,
      2,
    ),
  );
} finally {
  proc.stdin.end();
  proc.kill("SIGTERM");
}
