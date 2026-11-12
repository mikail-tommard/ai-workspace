import { spawn } from "node:child_process";
const server = spawn("./ai-workspace", [], { stdio: "inherit" });
const vite = spawn("npm", ["run", "dev"], { stdio: "inherit" });
let closing = false;
function close() {
  if (closing) return;
  closing = true;
  server.kill("SIGTERM");
  vite.kill("SIGTERM");
}
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, close);
server.on("exit", (code) => {
  close();
  process.exitCode = code || 0;
});
vite.on("exit", (code) => {
  close();
  process.exitCode = code || 0;
});
