import { useRef, useState } from "react";
import { Images, LoaderCircle, Upload } from "lucide-react";
import { uploadMedia } from "./store";
export default function MediaUpload() {
  const input = useRef<HTMLInputElement>(null);
  const [gallery, setGallery] = useState(false),
    [busy, setBusy] = useState(false),
    [drag, setDrag] = useState(false),
    [error, setError] = useState(""),
    [success, setSuccess] = useState("");
  async function upload(files: File[]) {
    if (!files.length || busy) return;
    setBusy(true);
    setError("");
    setSuccess("");
    try {
      await uploadMedia(files, gallery);
      setSuccess(
        gallery
          ? "Галерея добавлена на панель"
          : `Добавлено файлов: ${files.length}`,
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "Ошибка загрузки");
    } finally {
      setBusy(false);
      if (input.current) input.current.value = "";
    }
  }
  return (
    <section
      className={`media-upload ${drag ? "is-dragging" : ""}`}
      aria-label="Загрузка файлов"
      onDragOver={(e) => {
        e.preventDefault();
        setDrag(true);
      }}
      onDragLeave={() => setDrag(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDrag(false);
        void upload(Array.from(e.dataTransfer.files));
      }}
    >
      <div className="media-upload-symbol">
        {gallery ? <Images size={23} /> : <Upload size={23} />}
      </div>
      <strong>Ваши файлы — прямо на панели</strong>
      <p>
        Перетащите фотографии, видео, аудио или PDF.
        <br />
        До 25 МБ на файл. Файлы сохраняются на этом Mac.
      </p>
      <label className="media-gallery-option">
        <input
          type="checkbox"
          checked={gallery}
          disabled={busy}
          onChange={(e) => setGallery(e.target.checked)}
        />
        Собрать фотографии в галерею
      </label>
      <input
        ref={input}
        id="media-upload-input"
        className="sr-only"
        type="file"
        multiple
        accept={
          gallery
            ? "image/png,image/jpeg,image/gif,image/webp"
            : "image/png,image/jpeg,image/gif,image/webp,video/mp4,video/webm,audio/mpeg,audio/wav,audio/ogg,audio/mp4,.m4a,application/pdf"
        }
        aria-label="Выбрать локальные файлы"
        onChange={(e) => void upload(Array.from(e.target.files || []))}
      />
      <button
        className="quiet"
        disabled={busy}
        onClick={() => input.current?.click()}
      >
        {busy ? (
          <LoaderCircle size={16} className="spin" />
        ) : (
          <Upload size={16} />
        )}{" "}
        {busy ? "Загружаю…" : "Выбрать файлы"}
      </button>
      {error ? (
        <p role="alert" className="assistant-inline-error">
          {error}
        </p>
      ) : null}
      {success ? (
        <p role="status" className="upload-success">
          {success}
        </p>
      ) : null}
    </section>
  );
}
