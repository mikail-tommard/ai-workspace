import { useSyncExternalStore } from "react";
import type {
  ToolResult,
  Workspace,
  WorkspaceSummary,
  WireEvent,
} from "./types";

type Store = {
  workspace: Workspace | null;
  list: WorkspaceSummary[];
  connection: "connecting" | "online" | "offline";
  error: string | null;
};
let state: Store = {
  workspace: null,
  list: [],
  connection: "connecting",
  error: null,
};
const listeners = new Set<() => void>();
function set(patch: Partial<Store>) {
  state = { ...state, ...patch };
  listeners.forEach((f) => f());
}
export function useWorkspace() {
  return useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      return () => listeners.delete(cb);
    },
    () => state,
  );
}
let active = "default",
  socket: WebSocket | undefined,
  retry: ReturnType<typeof setTimeout> | undefined,
  generation = 0,
  failures = 0,
  stopped = false;
function accept(workspace: Workspace, force = false) {
  if (workspace.id !== active) return;
  const current = state.workspace;
  if (
    force ||
    !current ||
    current.id !== workspace.id ||
    workspace.version > current.version
  )
    set({ workspace, error: null });
}
export async function command(
  tool: string,
  args: Record<string, unknown> = {},
): Promise<ToolResult> {
  try {
    const response = await fetch("/api/commands", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "X-Workspace-Request": "1",
      },
      body: JSON.stringify({
        tool,
        arguments:
          tool === "workspace_create" || tool === "workspace_list"
            ? args
            : { workspace_id: active, ...args },
      }),
    });
    const result = await response.json();
    if (!response.ok)
      throw new Error(result.error || "Не удалось выполнить действие");
    if (result.state) accept(result.state);
    if (tool.startsWith("workspace_")) await refreshList();
    return result;
  } catch (e) {
    const message = e instanceof Error ? e.message : "Ошибка соединения";
    set({ error: message });
    throw e;
  }
}
export async function refreshList() {
  const r = await fetch("/api/workspaces");
  if (!r.ok) throw new Error("Не удалось загрузить рабочие пространства");
  const data = await r.json();
  set({ list: data.workspaces });
}
function reconnect(gen: number) {
  if (stopped || gen !== generation) return;
  set({ connection: "offline" });
  retry = setTimeout(
    () => void connect(gen),
    Math.min(15000, 500 * 2 ** failures++) + Math.random() * 250,
  );
}
async function connect(gen: number) {
  if (stopped || gen !== generation) return;
  set({ connection: "connecting" });
  try {
    // Refresh the HttpOnly session after server restarts or cookie expiry.
    const response = await fetch("/api/session");
    if (!response.ok) throw new Error("Session unavailable");
    await refreshList();
    if (stopped || gen !== generation) return;
    if (!state.list.some((w) => w.id === active)) {
      active = "default";
      localStorage.setItem("forma.workspace.v1", active);
      set({ workspace: null });
    }
  } catch {
    reconnect(gen);
    return;
  }
  if (stopped || gen !== generation) return;
  const url = new URL("/ws", location.href);
  url.protocol = location.protocol === "https:" ? "wss:" : "ws:";
  url.searchParams.set("workspace_id", active);
  const currentSocket = new WebSocket(url);
  socket = currentSocket;
  currentSocket.onopen = () => {
    if (gen !== generation) return;
    failures = 0;
    set({ connection: "online" });
  };
  currentSocket.onmessage = (message) => {
    if (gen !== generation) return;
    try {
      const event: WireEvent = JSON.parse(message.data);
      if (event.type === "workspace.deleted") {
        void selectWorkspace("default");
        void refreshList();
        return;
      }
      if (event.payload) accept(event.payload, event.type === "state.snapshot");
      if (event.type === "command.error")
        set({ error: event.error || "Ошибка команды" });
    } catch {
      set({ error: "Получено некорректное событие" });
    }
  };
  currentSocket.onclose = () => reconnect(gen);
  currentSocket.onerror = () => currentSocket.close();
}
export async function selectWorkspace(id: string) {
  active = id;
  generation++;
  clearTimeout(retry);
  socket?.close();
  set({ workspace: null, error: null });
  localStorage.setItem("forma.workspace.v1", id);
  connect(generation);
}
export async function start() {
  stopped = false;
  try {
    const response = await fetch("/api/session");
    if (!response.ok) throw new Error("Сервер недоступен");
    await refreshList();
    const saved = localStorage.getItem("forma.workspace.v1");
    const id = state.list.some((w) => w.id === saved) ? saved! : "default";
    await selectWorkspace(id);
  } catch (e) {
    set({
      connection: "offline",
      error: e instanceof Error ? e.message : "Сервер недоступен",
    });
  }
}
export function stop() {
  stopped = true;
  generation++;
  clearTimeout(retry);
  socket?.close();
}
export function clearError() {
  set({ error: null });
}
export async function uploadMedia(files: File[], gallery = false) {
  if (!files.length) return;
  if (files.length > 20)
    throw Error("За один раз можно загрузить до 20 файлов");
  if (gallery && files.some((file) => !file.type.startsWith("image/")))
    throw Error("Для галереи выберите только изображения");
  const workspaceId = active;
  const assets = [];
  for (const file of files) {
    if (file.size > 25 * 1024 * 1024)
      throw Error(`Файл «${file.name}» превышает 25 МБ`);
    const form = new FormData();
    form.append("file", file);
    form.append("attach", gallery ? "false" : "true");
    const response = await fetch(
      `/api/media/${encodeURIComponent(workspaceId)}`,
      { method: "POST", headers: { "X-Workspace-Request": "1" }, body: form },
    );
    if (response.status === 401)
      throw Error("Сессия истекла. Обновите страницу и повторите загрузку.");
    if (response.status === 404)
      throw Error(
        "В работающем сервере ещё нет загрузки файлов. Перезапустите новый бинарник и обновите страницу.",
      );
    let result: ToolResult & { error?: string };
    try {
      result = await response.json();
    } catch {
      throw Error(
        "Не удалось загрузить файл. Проверьте размер и версию сервера.",
      );
    }
    if (!response.ok) throw Error(result.error || "Не удалось загрузить файл");
    if (result.asset) assets.push(result.asset);
    if (result.state) accept(result.state);
  }
  if (gallery && assets.length) {
    await command("widget_create", {
      workspace_id: workspaceId,
      type: "gallery",
      title: "Галерея",
      config: {
        images: assets.map((asset) => ({ url: asset.url, alt: asset.name })),
      },
    });
  }
  await refreshList();
}
