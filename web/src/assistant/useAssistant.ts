import { useSyncExternalStore } from "react";
import { AssistantController } from "./controller";
export const assistant = new AssistantController();
export function useAssistant() {
  return useSyncExternalStore(assistant.subscribe, assistant.snapshot);
}
