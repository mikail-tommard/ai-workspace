export type AssistantStatus =
  | "disconnected"
  | "connecting"
  | "listening"
  | "thinking"
  | "responding"
  | "error";
export type AssistantSettings = {
  mode: "notifications";
  notifications: boolean;
  language: "ru" | "en";
  save_history: boolean;
  speech_pause_ms: number;
};
export type DesktopStatus = {
  supported: boolean;
  engine: string;
  last_status: string;
  last_error?: string;
  note: string;
};
export function normalizeSettings(
  s: Partial<AssistantSettings>,
): AssistantSettings {
  return {
    speech_pause_ms:
      s.speech_pause_ms &&
      s.speech_pause_ms >= 2000 &&
      s.speech_pause_ms <= 10000
        ? s.speech_pause_ms
        : 4000,
    mode: "notifications",
    notifications: s.notifications ?? true,
    language: s.language === "en" ? "en" : "ru",
    save_history: s.save_history ?? true,
  };
}
export type ChatMessage = {
  id: string;
  role: "user" | "assistant";
  text: string;
  at: string;
};
export type PendingConfirmation = { call_id: string; label: string };
export type AssistantState = {
  workspaceId: string;
  status: AssistantStatus;
  connected: boolean;
  microphone: boolean;
  configured: boolean | null;
  configurationMessage: string;
  settings: AssistantSettings;
  messages: ChatMessage[];
  activity: string;
  desktop: DesktopStatus | null;
  notificationError: string | null;
  pending: PendingConfirmation | null;
  error: string | null;
  interim: string;
  voiceDraft: string;
};
export type RealtimeEvent = {
  type: string;
  event_id?: string;
  item_id?: string;
  response_id?: string;
  transcript?: string;
  text?: string;
  delta?: string;
  error?: { code?: string; message?: string };
  response?: {
    id: string;
    metadata?: Record<string, string>;
    status?: string;
    status_details?: { error?: { message?: string } };
    output?: RealtimeItem[];
  };
};
export type RealtimeItem = {
  type: string;
  id?: string;
  call_id?: string;
  name?: string;
  arguments?: string;
  content?: { type: string; text?: string; transcript?: string }[];
};
export type RoutedCall = {
  call_id: string;
  name: string;
  arguments: Record<string, unknown>;
  confirmation?: string;
  decision?: "approve" | "reject";
};
export type RoutedReply = {
  status: "success" | "error" | "confirmation_required" | "cancelled";
  result?: Record<string, unknown>;
  error?: string;
  confirmation?: string;
  label?: string;
};
export const defaultSettings: AssistantSettings = {
  mode: "notifications",
  notifications: true,
  language: "ru",
  save_history: true,
  speech_pause_ms: 4000,
};
