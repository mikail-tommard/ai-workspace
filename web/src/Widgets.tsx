import { lazy, Suspense, useState } from "react";
import {
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Code2,
  FileText,
  Grip,
  Info,
  ImageIcon,
  Images,
  Video,
  Music,
  CheckSquare,
  Gauge,
  TrendingUp,
  Paperclip,
  Lightbulb,
  MoveRight,
  Sigma,
  Table2,
  Trash2,
  BarChart3,
  ListChecks,
  LoaderCircle,
} from "lucide-react";
import type { Problem, Widget } from "./types";
import { command, useWorkspace } from "./store";
import MathFormula from "./MathFormula";
import {
  ImageWidget,
  GalleryWidget,
  ChecklistWidget,
  MetricWidget,
  ProgressWidget,
  FileWidget,
} from "./MediaWidgets";
import Markdown from "./Markdown";
const Chart = lazy(() => import("./Chart"));
const CodeBlock = lazy(() => import("./CodeBlock"));
const typeLabels = {
  math_problem: "Математика",
  quiz: "Тест",
  chart: "График",
  markdown: "Документ",
  image: "Изображение",
  gallery: "Галерея",
  video: "Видео",
  audio: "Аудио",
  file: "Вложение",
  checklist: "Чек-лист",
  metric: "Показатель",
  progress: "Прогресс",
  code: "Код",
  table: "Таблица",
  notification: "Сообщение",
};
const typeIcons = {
  math_problem: Sigma,
  quiz: ListChecks,
  chart: BarChart3,
  markdown: FileText,
  image: ImageIcon,
  gallery: Images,
  video: Video,
  audio: Music,
  file: Paperclip,
  checklist: CheckSquare,
  metric: TrendingUp,
  progress: Gauge,
  code: Code2,
  table: Table2,
  notification: Info,
};
export function MathProblem({
  problem,
  widgetId,
  index,
}: {
  problem: Problem;
  widgetId: string;
  index?: number;
}) {
  const [answer, setAnswer] = useState(""),
    [hint, setHint] = useState(false),
    [busy, setBusy] = useState(false);
  const last = problem.attempts.at(-1);
  const extra = index === undefined ? {} : { question_index: index };
  async function check(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      await command("math_check_answer", {
        widget_id: widgetId,
        answer,
        ...extra,
      });
    } catch {
      /* Global error banner provides recovery. */
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="math-problem">
      <div className="problem-meta">
        <span className={`difficulty ${problem.difficulty}`}>
          {
            { easy: "Начальный", medium: "Средний", hard: "Продвинутый" }[
              problem.difficulty
            ]
          }
        </span>
        <span>
          {
            {
              arithmetic: "Арифметика",
              linear: "Линейное уравнение",
              quadratic: "Квадратное уравнение",
              system: "Система уравнений",
              percent: "Проценты",
              fraction: "Дроби",
            }[problem.kind]
          }
        </span>
      </div>
      <p className="problem-prompt">{problem.prompt}</p>
      <MathFormula latex={problem.latex} />
      <form onSubmit={check} className="answer-form">
        <label htmlFor={`answer-${widgetId}-${index ?? 0}`}>Ваш ответ</label>
        <div className="answer-row">
          <input
            id={`answer-${widgetId}-${index ?? 0}`}
            value={answer}
            onChange={(e) => setAnswer(e.target.value)}
            placeholder={
              problem.kind === "quadratic" ? "Например: 2; 3" : "Введите ответ"
            }
            required
            maxLength={512}
            autoComplete="off"
          />
          <button
            className="primary"
            disabled={busy || !answer.trim()}
            type="submit"
          >
            {busy ? (
              <LoaderCircle className="spin" size={16} />
            ) : (
              <Check size={16} />
            )}
            Проверить
          </button>
        </div>
      </form>
      {last ? (
        <div
          role="status"
          className={`answer-feedback ${last.correct ? "correct" : "incorrect"}`}
        >
          {last.correct ? <CheckCircle2 size={17} /> : <Info size={17} />}{" "}
          {last.correct
            ? "Верно! Отличная работа."
            : "Пока неверно. Попробуйте ещё раз."}
          <span>Ответ: {last.answer}</span>
        </div>
      ) : null}
      <div className="problem-actions">
        <button className="text-button" onClick={() => setHint(!hint)}>
          <Lightbulb size={15} />
          {hint ? "Скрыть подсказку" : "Подсказка"}
        </button>
        <button
          className="text-button"
          onClick={() =>
            void command("math_show_solution", {
              widget_id: widgetId,
              ...extra,
            }).catch(() => {})
          }
        >
          Показать решение
          <ArrowRight size={14} />
        </button>
      </div>
      {hint ? <aside className="hint">{problem.hints.join(" ")}</aside> : null}
      {problem.steps ? (
        <ol className="solution">
          {problem.steps.map((step, i) => (
            <li key={i}>{step}</li>
          ))}
        </ol>
      ) : null}
    </div>
  );
}
function Table({ widget }: { widget: Extract<Widget, { type: "table" }> }) {
  const [search, setSearch] = useState(""),
    [sort, setSort] = useState<{ col: number; dir: 1 | -1 }>({
      col: 0,
      dir: 1,
    });
  const rows = widget.config.rows
    .filter((row) =>
      row.some((cell) =>
        cell.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
      ),
    )
    .toSorted(
      (a, b) =>
        a[sort.col].localeCompare(b[sort.col], undefined, { numeric: true }) *
        sort.dir,
    );
  return (
    <div className="table-widget">
      <label className="sr-only" htmlFor={`search-${widget.id}`}>
        Поиск в таблице
      </label>
      <input
        id={`search-${widget.id}`}
        placeholder="Поиск в таблице…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
      />
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              {widget.config.columns.map((col, i) => (
                <th
                  key={i}
                  scope="col"
                  aria-sort={
                    sort.col === i
                      ? sort.dir === 1
                        ? "ascending"
                        : "descending"
                      : "none"
                  }
                >
                  <button
                    onClick={() =>
                      setSort({
                        col: i,
                        dir: sort.col === i && sort.dir === 1 ? -1 : 1,
                      })
                    }
                  >
                    {col}
                    {sort.col === i ? (
                      sort.dir === 1 ? (
                        <ChevronUp size={13} />
                      ) : (
                        <ChevronDown size={13} />
                      )
                    ) : null}
                  </button>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i}>
                {row.map((cell, j) => (
                  <td key={j}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted small">
        {rows.length} из {widget.config.rows.length} строк
      </p>
    </div>
  );
}
function Quiz({ widget }: { widget: Extract<Widget, { type: "quiz" }> }) {
  const [index, setIndex] = useState(0);
  const solved = widget.questions.filter((p) =>
    p.attempts.some((a) => a.correct),
  ).length;
  return (
    <div>
      <div className="quiz-progress">
        <span>
          Вопрос {index + 1} из {widget.questions.length}
        </span>
        <span>{solved} решено</span>
      </div>
      <progress value={solved} max={widget.questions.length} />
      <MathProblem
        key={index}
        problem={widget.questions[index]}
        widgetId={widget.id}
        index={index}
      />
      <div className="quiz-navigation">
        <button disabled={index === 0} onClick={() => setIndex(index - 1)}>
          Назад
        </button>
        <button
          disabled={index === widget.questions.length - 1}
          onClick={() => setIndex(index + 1)}
        >
          Следующий <ArrowRight size={14} />
        </button>
      </div>
    </div>
  );
}
function Body({ widget }: { widget: Widget }) {
  switch (widget.type) {
    case "gallery":
      return <GalleryWidget images={widget.config.images} />;
    case "video":
      return (
        <figure className="media-widget">
          <video
            src={widget.config.url}
            controls
            preload="metadata"
            aria-label={widget.title}
          />
          {widget.config.caption ? (
            <figcaption>{widget.config.caption}</figcaption>
          ) : null}
        </figure>
      );
    case "audio":
      return (
        <figure className="media-widget">
          <audio
            src={widget.config.url}
            controls
            preload="metadata"
            aria-label={widget.title}
          />
          {widget.config.caption ? (
            <figcaption>{widget.config.caption}</figcaption>
          ) : null}
        </figure>
      );
    case "file":
      return <FileWidget config={widget.config} />;
    case "checklist":
      return <ChecklistWidget widget={widget} />;
    case "metric":
      return <MetricWidget config={widget.config} />;
    case "progress":
      return <ProgressWidget config={widget.config} />;

    case "image":
      return <ImageWidget key={widget.config.url} config={widget.config} />;
    case "math_problem":
      return (
        <MathProblem
          key={`${widget.id}-${widget.problem.latex}`}
          problem={widget.problem}
          widgetId={widget.id}
        />
      );
    case "quiz":
      return <Quiz widget={widget} />;
    case "chart":
      return <Chart config={widget.config} />;
    case "markdown":
      return <Markdown content={widget.config.content} />;
    case "code":
      return (
        <CodeBlock
          code={widget.config.code}
          language={widget.config.language}
        />
      );
    case "table":
      return <Table widget={widget} />;
    case "notification":
      return (
        <div className={`notification ${widget.config.level}`}>
          <Info size={22} />
          <p>{widget.config.message}</p>
        </div>
      );
  }
}
export default function WidgetCard({ widget }: { widget: Widget }) {
  const Icon = typeIcons[widget.type],
    { workspace } = useWorkspace(),
    [menu, setMenu] = useState(false),
    [confirm, setConfirm] = useState(false);
  return (
    <article
      className={`widget-card widget-${widget.type}`}
      data-widget-id={widget.id}
    >
      <div className="widget-heading">
        <div className="widget-type">
          <Icon size={15} />
          {typeLabels[widget.type]}
        </div>
        <div className="widget-controls">
          <button
            className="icon-button"
            aria-label={`Действия: ${widget.title}`}
            aria-expanded={menu}
            onClick={() => setMenu(!menu)}
          >
            <Grip size={17} />
          </button>
        </div>
      </div>
      <h2>{widget.title}</h2>
      {menu ? (
        <div className="widget-menu">
          <button
            onClick={() =>
              void command("widget_move", {
                widget_id: widget.id,
                position: Math.max(0, widget.position - 1),
              }).catch(() => {})
            }
          >
            <ChevronUp size={14} />
            Выше
          </button>
          <label>
            <MoveRight size={14} />
            <select
              aria-label="Переместить во вкладку"
              value={widget.tab_id}
              onChange={(e) =>
                void command("widget_move", {
                  widget_id: widget.id,
                  position: widget.position,
                  tab_id: e.target.value,
                }).catch(() => {})
              }
            >
              {workspace?.tabs.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
          <button className="danger" onClick={() => setConfirm(true)}>
            <Trash2 size={14} />
            Удалить
          </button>
        </div>
      ) : null}
      {confirm ? (
        <div className="inline-confirm">
          Удалить карточку?
          <button
            className="danger"
            onClick={() =>
              void command("widget_delete", { widget_id: widget.id }).catch(
                () => {},
              )
            }
          >
            Удалить
          </button>
          <button onClick={() => setConfirm(false)}>Отмена</button>
        </div>
      ) : null}
      <Suspense fallback={<div className="skeleton widget-skeleton" />}>
        <Body widget={widget} />
      </Suspense>
      <div className="widget-footer">
        <span className="tiny-dot" /> Сохранено локально
        <span className="mono">{widget.id.slice(0, 6)}</span>
      </div>
    </article>
  );
}
