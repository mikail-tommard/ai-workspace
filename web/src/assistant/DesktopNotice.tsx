import { Bell, Monitor, ArrowUpRight } from "lucide-react";
import { assistant, useAssistant } from "./useAssistant";
export default function DesktopNotice() {
  const state = useAssistant();
  return (
    <div className="desktop-notice">
      <span className="desktop-notice-icon">
        <Bell size={20} />
      </span>
      <div>
        <strong>Ответы — в Центре уведомлений</strong>
        <p>
          Тихие системные баннеры macOS. Полный текст всегда остаётся в истории
          диалога.
        </p>
        <button
          type="button"
          className="text-button"
          disabled={!state.desktop?.supported}
          onClick={() => void assistant.testNotification()}
        >
          Проверить уведомление macOS <ArrowUpRight size={14} />
        </button>
        <small>
          <Monitor size={12} />
          {state.desktop?.supported
            ? "macOS · без озвучки"
            : "Доступно на компьютере с macOS"}
        </small>
        {state.desktop?.last_status === "submitted" ? (
          <p role="status">
            Уведомление передано macOS. Если баннера нет, проверьте настройки
            уведомлений Script Editor и «Фокусирование».
          </p>
        ) : null}
        {state.notificationError ? (
          <p className="assistant-inline-error" role="alert">
            {state.notificationError}
          </p>
        ) : null}
      </div>
    </div>
  );
}
