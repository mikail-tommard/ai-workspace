import { useEffect, useRef, useState } from "react";
import { Check, Copy } from "lucide-react";
export function CopyButton({ text }: { text: string }) {
  const [status, setStatus] = useState<"ready" | "copied" | "error">("ready");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  async function copy() {
    clearTimeout(timer.current);
    try {
      await navigator.clipboard.writeText(text);
      setStatus("copied");
    } catch {
      setStatus("error");
    }
    timer.current = setTimeout(() => setStatus("ready"), 1800);
  }
  return (
    <button
      className="copy-button"
      onClick={() => void copy()}
      aria-label="Скопировать код"
    >
      {status === "copied" ? <Check size={14} /> : <Copy size={14} />}
      {status === "copied"
        ? "Скопировано"
        : status === "error"
          ? "Нет доступа к буферу"
          : "Копировать"}
    </button>
  );
}
