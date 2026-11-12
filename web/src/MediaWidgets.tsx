import { useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  ArrowDownRight,
  ArrowUpRight,
  Download,
  FileText,
  ImageIcon,
  Minus,
  X,
} from "lucide-react";
import type { Widget, MediaImage } from "./types";
import { command } from "./store";
export function ImageWidget({ config }: { config: MediaImage }) {
  const [failed, setFailed] = useState(false);
  return (
    <figure className="image-widget">
      {failed ? (
        <p role="status">
          Изображение не загрузилось.{" "}
          <a href={config.url} target="_blank" rel="noopener noreferrer">
            Открыть файл
          </a>
        </p>
      ) : (
        <Dialog.Root>
          <Dialog.Trigger
            className="image-open"
            aria-label={`Открыть изображение: ${config.alt}`}
          >
            <img
              src={config.url}
              alt={config.alt}
              loading="lazy"
              decoding="async"
              referrerPolicy="no-referrer"
              onError={() => setFailed(true)}
            />
            <span>
              <ImageIcon size={13} />
              Увеличить
            </span>
          </Dialog.Trigger>
          <Dialog.Portal>
            <Dialog.Overlay className="dialog-overlay" />
            <Dialog.Content className="dialog-content image-lightbox">
              <div className="image-lightbox-header">
                <Dialog.Title>{config.alt}</Dialog.Title>
                <Dialog.Close
                  className="icon-button"
                  aria-label="Закрыть изображение"
                >
                  <X size={20} />
                </Dialog.Close>
              </div>
              <Dialog.Description className="sr-only">
                Просмотр изображения в полном размере
              </Dialog.Description>
              <img
                src={config.url}
                alt={config.alt}
                referrerPolicy="no-referrer"
              />
              <a
                className="media-download"
                href={config.url}
                target="_blank"
                rel="noopener noreferrer"
                download
              >
                <Download size={15} />
                Открыть или скачать
              </a>
            </Dialog.Content>
          </Dialog.Portal>
        </Dialog.Root>
      )}
      <figcaption>{config.caption || config.alt}</figcaption>
    </figure>
  );
}
export function GalleryWidget({ images }: { images: MediaImage[] }) {
  return (
    <div className="gallery-grid">
      {images.map((image, i) => (
        <ImageWidget key={image.url + ":" + i} config={image} />
      ))}
    </div>
  );
}
export function ChecklistWidget({
  widget,
}: {
  widget: Extract<Widget, { type: "checklist" }>;
}) {
  const [busy, setBusy] = useState(false);
  const done = widget.config.items.filter((i) => i.done).length;
  async function toggle(id: string) {
    setBusy(true);
    try {
      await command("widget_update", {
        widget_id: widget.id,
        config: {
          items: widget.config.items.map((item) =>
            item.id === id ? { ...item, done: !item.done } : item,
          ),
        },
      });
    } catch {
      /* Shared error banner. */
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="checklist-widget">
      <p className="checklist-summary">
        {done} из {widget.config.items.length} выполнено
      </p>
      {widget.config.items.length ? (
        widget.config.items.map((item) => (
          <label key={item.id} className={item.done ? "is-done" : ""}>
            <input
              type="checkbox"
              checked={item.done}
              disabled={busy}
              onChange={() => void toggle(item.id)}
            />
            <span>{item.text}</span>
          </label>
        ))
      ) : (
        <p className="muted">Список пуст. Попросите Codex добавить пункты.</p>
      )}
    </div>
  );
}
export function MetricWidget({
  config,
}: {
  config: Extract<Widget, { type: "metric" }>["config"];
}) {
  const Icon =
    config.trend === "up"
      ? ArrowUpRight
      : config.trend === "down"
        ? ArrowDownRight
        : Minus;
  return (
    <div className="metric-widget">
      <div>
        <strong>{config.value}</strong>
        {config.unit ? <span>{config.unit}</span> : null}
      </div>
      {config.label ? <p>{config.label}</p> : null}
      {config.change ? (
        <small className={`metric-change ${config.trend || "neutral"}`}>
          <Icon size={14} />
          {config.change}
        </small>
      ) : null}
    </div>
  );
}
export function ProgressWidget({
  config,
}: {
  config: Extract<Widget, { type: "progress" }>["config"];
}) {
  return (
    <div className="progress-widget">
      <div>
        <span>{config.label || "Выполнение"}</span>
        <strong>{config.value}%</strong>
      </div>
      <progress
        aria-label={config.label || "Прогресс"}
        value={config.value}
        max={100}
      />
      {config.detail ? <p>{config.detail}</p> : null}
    </div>
  );
}
export function FileWidget({
  config,
}: {
  config: Extract<Widget, { type: "file" }>["config"];
}) {
  return (
    <div className="file-widget">
      <span className="file-icon">
        <FileText size={28} />
      </span>
      <div>
        <strong>{config.filename}</strong>
        {config.description ? <p>{config.description}</p> : null}
        <a
          className="media-download"
          href={config.url}
          target="_blank"
          rel="noopener noreferrer"
          download
        >
          <Download size={14} />
          Скачать файл
        </a>
      </div>
    </div>
  );
}
