import { useEffect, useState } from "react";
import * as Dialog from "@radix-ui/react-dialog";
import {
  ArrowDownToLine,
  ArrowRight,
  BarChart3,
  CheckCircle2,
  ChevronDown,
  Command,
  FileText,
  Folder,
  Layers,
  LayoutGrid,
  List,
  Menu,
  Moon,
  Plus,
  Search,
  Settings2,
  Sigma,
  Sun,
  Terminal,
  Trash2,
  Wifi,
  WifiOff,
  X,
} from "lucide-react";
import {
  clearError,
  command,
  refreshList,
  selectWorkspace,
  start,
  stop,
  useWorkspace,
} from "./store";
import type { MathKind } from "./types";
import WidgetCard from "./Widgets";
import AssistantPanel from "./assistant/AssistantPanel";
import MediaUpload from "./MediaUpload";

const samples = {
  checklist: () =>
    command("widget_create", {
      type: "checklist",
      title: "План действий",
      config: {
        items: [
          { id: "goal", text: "Сформулировать цель", done: false },
          { id: "research", text: "Собрать материалы", done: false },
          { id: "result", text: "Проверить результат", done: false },
        ],
      },
    }),
  metric: () =>
    command("widget_create", {
      type: "metric",
      title: "Показатель",
      config: {
        value: "42",
        label: "Пример числового показателя",
        unit: "ед.",
        trend: "neutral",
      },
    }),
  progress: () =>
    command("widget_create", {
      type: "progress",
      title: "Прогресс проекта",
      config: {
        value: 0,
        label: "Работа ещё не начата",
        detail: "Попросите Codex обновить значение по результату работы.",
      },
    }),
  chart: () =>
    command("chart_create", {
      title: "Функция y = x²",
      config: {
        kind: "line",
        x_label: "x",
        y_label: "y",
        points: Array.from({ length: 21 }, (_, i) => ({
          x: i - 10,
          y: (i - 10) ** 2,
        })),
      },
    }),
  table: () =>
    command("widget_create", {
      type: "table",
      title: "План исследования",
      config: {
        columns: ["Этап", "Статус", "Приоритет"],
        rows: [
          ["Сформулировать гипотезу", "Готово", "Высокий"],
          ["Собрать данные", "В работе", "Высокий"],
          ["Проанализировать результаты", "Запланировано", "Средний"],
        ],
      },
    }),
  markdown: () =>
    command("markdown_render", {
      title: "Заметки к исследованию",
      content:
        "# Пространство для идей\nСоздавайте документы, графики и задачи вместе с AI.\n\n## Следующие шаги\n- Сформулировать вопрос\n- Исследовать данные\n- Записать выводы\n\n> Все изменения сохраняются на этом компьютере.",
    }),
  code: () =>
    command("code_create", {
      title: "Hello, workspace",
      language: "go",
      code: 'package main\n\nimport "fmt"\n\nfunc main() {\n    fmt.Println("Hello, workspace!")\n}',
    }),
  quiz: () =>
    command("math_generate_quiz", {
      title: "Практика: линейные уравнения",
      kind: "linear",
      difficulty: "medium",
      count: 5,
    }),
  notification: () =>
    command("ui_show_notification", {
      title: "Готово к работе",
      message:
        "Это сообщение пришло через тот же сервис команд, который использует MCP. Подключите Codex, чтобы создавать здесь собственные материалы.",
      level: "info",
    }),
};
export default function App() {
  const { workspace: ws, list, connection, error } = useWorkspace();
  const [sidebar, setSidebar] = useState(false),
    [search, setSearch] = useState(""),
    [dialog, setDialog] = useState<
      "create" | "workspace" | "tab" | "connect" | "clear" | "delete" | null
    >(null),
    [name, setName] = useState(""),
    [kind, setKind] = useState<MathKind>("quadratic"),
    [difficulty, setDifficulty] = useState("medium"),
    [busy, setBusy] = useState(false);
  useEffect(() => {
    void start();
    return stop;
  }, []);
  useEffect(() => {
    const theme = ws?.theme || "system",
      media = matchMedia("(prefers-color-scheme: dark)");
    const apply = () =>
      (document.documentElement.dataset.theme =
        theme === "system" ? (media.matches ? "dark" : "light") : theme);
    apply();
    media.addEventListener("change", apply);
    return () => media.removeEventListener("change", apply);
  }, [ws?.theme]);
  async function action(fn: () => Promise<unknown>) {
    setBusy(true);
    try {
      await fn();
      setDialog(null);
      setName("");
    } catch {
      /* Error banner is shared across commands. */
    } finally {
      setBusy(false);
    }
  }
  const widgets =
    ws?.widgets
      .filter(
        (w) =>
          w.tab_id === ws.active_tab &&
          w.title.toLocaleLowerCase().includes(search.toLocaleLowerCase()),
      )
      .toSorted(
        (a, b) => a.position - b.position || a.id.localeCompare(b.id),
      ) || [];
  const solved =
    ws?.widgets.filter(
      (w) =>
        w.type === "math_problem" && w.problem.attempts.some((a) => a.correct),
    ).length || 0;
  const online = connection === "online";
  function open(d: typeof dialog) {
    setName("");
    setDialog(d);
    setSidebar(false);
  }
  return (
    <div className="app-shell">
      <a className="skip-link" href="#workspace-main">
        К рабочей области
      </a>
      {sidebar ? (
        <button
          className="sidebar-backdrop"
          aria-label="Закрыть навигацию"
          onClick={() => setSidebar(false)}
        />
      ) : null}
      <aside className={`sidebar ${sidebar ? "is-open" : ""}`}>
        <a className="brand" href="/" aria-label="Forma — главная">
          <span className="brand-mark">f</span>forma
          <span className="brand-badge">LOCAL</span>
        </a>
        <div className="workspace-picker">
          <div className="workspace-avatar">{ws?.name[0] || "F"}</div>
          <div>
            <span className="overline">Рабочее пространство</span>
            <strong>{ws?.name || "Загрузка…"}</strong>
          </div>
          <ChevronDown size={15} />
        </div>
        <div className="sidebar-label">
          ПРОСТРАНСТВА
          <button
            className="icon-button"
            aria-label="Создать пространство"
            onClick={() => open("workspace")}
          >
            <Plus size={15} />
          </button>
        </div>
        <nav aria-label="Рабочие пространства">
          {list.map((w) => (
            <button
              key={w.id}
              className={`nav-item ${w.id === ws?.id ? "selected" : ""}`}
              onClick={() => {
                void selectWorkspace(w.id);
                setSidebar(false);
              }}
            >
              <Folder size={17} />
              <span>{w.name}</span>
              <span className="nav-count">
                {w.id === ws?.id ? ws.widgets.length : w.widget_count}
              </span>
            </button>
          ))}
        </nav>
        <div className="sidebar-label section-gap">ИНСТРУМЕНТЫ</div>
        <button className="nav-item" onClick={() => open("create")}>
          <Layers size={17} />
          Библиотека виджетов
          <Plus size={14} />
        </button>
        <button className="nav-item" onClick={() => open("connect")}>
          <Terminal size={17} />
          Подключить Codex
          <ArrowRight size={14} />
        </button>
        <div className="sidebar-bottom">
          <div className="local-note">
            <span className="local-icon">
              <CheckCircle2 size={18} />
            </span>
            <div>
              <strong>Ваши данные — у вас</strong>
              <p>SQLite · на этом компьютере</p>
            </div>
          </div>
          <div className="sidebar-bottom-row">
            <span className="user-avatar">Л</span>
            <div>
              <strong>Локальный пользователь</strong>
              <span>Персональное пространство</span>
            </div>
            <button
              className="icon-button"
              aria-label="Переключить тему"
              onClick={() =>
                void command("ui_set_layout", {
                  theme:
                    document.documentElement.dataset.theme === "dark"
                      ? "light"
                      : "dark",
                }).catch(() => {})
              }
            >
              {ws?.theme === "dark" ? <Sun size={17} /> : <Moon size={17} />}
            </button>
          </div>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumbs">
            <button
              className="icon-button mobile-menu"
              aria-label="Открыть навигацию"
              onClick={() => setSidebar(true)}
            >
              <Menu size={20} />
            </button>
            <Layers size={17} />
            <span>Workspace</span>
            <span className="breadcrumb-slash">/</span>
            <strong>{ws?.name || "Подключение"}</strong>
          </div>
          <div className="topbar-actions">
            <span
              className={`connection ${online ? "online" : ""}`}
              role="status"
            >
              {online ? <Wifi size={14} /> : <WifiOff size={14} />}
              <span>
                {online
                  ? "Подключено"
                  : connection === "connecting"
                    ? "Подключение…"
                    : "Переподключение…"}
              </span>
            </span>
            <button
              className="quiet small-button"
              onClick={() => open("connect")}
            >
              <Command size={14} />
              <span>MCP</span>
            </button>
          </div>
        </header>
        <main id="workspace-main">
          <div className="page-title-row">
            <div>
              <div className="eyebrow">
                <span /> МЕСТО, ГДЕ ИДЕИ ОБРЕТАЮТ ФОРМУ
              </div>
              <h1>{ws?.name || "Ваше рабочее пространство"}</h1>
              <p className="subtitle">
                Думайте вместе с AI. Исследуйте, создавайте, находите ответы.
              </p>
            </div>
            <button
              className="primary add-widget"
              onClick={() => open("create")}
              disabled={!ws}
            >
              <Plus size={17} />
              Добавить виджет
            </button>
          </div>
          <div className="summary-row">
            <div>
              <span className="summary-icon">
                <Layers size={16} />
              </span>
              <strong>{ws?.widgets.length || 0}</strong>
              <span>виджетов</span>
            </div>
            <div>
              <span className="summary-icon">
                <Folder size={16} />
              </span>
              <strong>{ws?.tabs.length || 1}</strong>
              <span>вкладок</span>
            </div>
            <div>
              <span className="summary-icon">
                <CheckCircle2 size={16} />
              </span>
              <strong>{solved}</strong>
              <span>задач решено</span>
            </div>
            <span className="saved-label">
              <span className="tiny-dot" />
              Автосохранение включено
            </span>
          </div>
          {error ? (
            <div className="error-banner" role="alert">
              <span>{error}</span>
              <button
                className="text-button"
                onClick={() => {
                  clearError();
                  if (!ws) void start();
                }}
              >
                Повторить
              </button>
              <button
                className="icon-button"
                aria-label="Скрыть ошибку"
                onClick={clearError}
              >
                <X size={16} />
              </button>
            </div>
          ) : null}
          <div className="workspace-toolbar">
            <div
              className="tabs"
              role="tablist"
              aria-label="Вкладки пространства"
            >
              {ws?.tabs.map((t) => (
                <button
                  key={t.id}
                  role="tab"
                  aria-selected={t.id === ws.active_tab}
                  className={t.id === ws.active_tab ? "active" : ""}
                  onClick={() =>
                    void command("ui_switch_tab", { tab_id: t.id }).catch(
                      () => {},
                    )
                  }
                >
                  {t.id === "main" ? (
                    <LayoutGrid size={15} />
                  ) : (
                    <Folder size={15} />
                  )}{" "}
                  {t.title}
                  <span>
                    {ws.widgets.filter((w) => w.tab_id === t.id).length}
                  </span>
                </button>
              ))}
              <button
                className="icon-button"
                aria-label="Добавить вкладку"
                onClick={() => open("tab")}
                disabled={!ws}
              >
                <Plus size={16} />
              </button>
            </div>
            <div className="view-options">
              <label className="search-box">
                <Search size={15} />
                <input
                  aria-label="Найти виджет"
                  placeholder="Найти виджет…"
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                />
              </label>
              <div
                className="layout-toggle"
                aria-label="Вид рабочего пространства"
              >
                <button
                  aria-label="Сетка"
                  aria-pressed={ws?.layout === "grid"}
                  onClick={() =>
                    void command("ui_set_layout", { layout: "grid" }).catch(
                      () => {},
                    )
                  }
                >
                  <LayoutGrid size={15} />
                </button>
                <button
                  aria-label="Список"
                  aria-pressed={ws?.layout === "list"}
                  onClick={() =>
                    void command("ui_set_layout", { layout: "list" }).catch(
                      () => {},
                    )
                  }
                >
                  <List size={17} />
                </button>
              </div>
            </div>
          </div>
          {!ws ? (
            <div className="widget-grid">
              {[0, 1, 2, 3].map((i) => (
                <div key={i} className="skeleton loading-card" />
              ))}
            </div>
          ) : widgets.length ? (
            <div
              className={`widget-grid ${ws.layout === "list" ? "list-layout" : ""}`}
            >
              {widgets.map((w) => (
                <WidgetCard key={w.id} widget={w} />
              ))}
            </div>
          ) : (
            <section className="empty-state">
              <div className="empty-illustration">
                <span className="illus-card card-back">
                  <BarChart3 size={29} />
                </span>
                <span className="illus-card card-front">
                  <Sigma size={40} />
                </span>
                <span className="illus-spark">+</span>
              </div>
              <span className="eyebrow">ВАШ СЛЕДУЮЩИЙ ШАГ</span>
              <h2>
                {search ? "Ничего не найдено" : "Большие идеи начинаются здесь"}
              </h2>
              <p>
                {search
                  ? "Попробуйте другое название виджета."
                  : "Попросите Codex создать задачу, показать график или собрать ваши идеи в документ. Всё появится здесь в реальном времени."}
              </p>
              <div className="empty-actions">
                <button className="primary" onClick={() => open("create")}>
                  <Plus size={16} />
                  Создать первый виджет
                </button>
                <button className="quiet" onClick={() => open("connect")}>
                  Подключить Codex
                  <ArrowRight size={15} />
                </button>
              </div>
              <div className="quick-prompts">
                <button
                  onClick={() =>
                    void action(() =>
                      command("math_create_problem", {
                        kind: "quadratic",
                        difficulty: "medium",
                      }),
                    )
                  }
                >
                  <Sigma size={16} />
                  Создать математическую задачу
                  <ArrowRight size={14} />
                </button>
                <button onClick={() => void action(samples.chart)}>
                  <BarChart3 size={16} />
                  Показать график функции
                  <ArrowRight size={14} />
                </button>
                <button onClick={() => void action(samples.markdown)}>
                  <FileText size={16} />
                  Записать новую идею
                  <ArrowRight size={14} />
                </button>
              </div>
            </section>
          )}
          <div className="workspace-footer">
            <span>
              <span className="tiny-dot" />
              SQLite · версия {ws?.version || "—"}
            </span>
            <div>
              <button
                className="text-button"
                onClick={() => {
                  if (!ws) return;
                  const a = document.createElement("a");
                  const url = URL.createObjectURL(
                    new Blob([JSON.stringify(ws, null, 2)], {
                      type: "application/json",
                    }),
                  );
                  a.href = url;
                  a.download = `${ws.id}.json`;
                  a.click();
                  URL.revokeObjectURL(url);
                }}
                disabled={!ws}
              >
                <ArrowDownToLine size={14} />
                Экспорт
              </button>
              <button
                className="text-button"
                onClick={() => open("clear")}
                disabled={!ws?.widgets.length}
              >
                <Trash2 size={14} />
                Очистить
              </button>
              {ws?.id !== "default" ? (
                <button
                  className="text-button danger"
                  onClick={() => open("delete")}
                >
                  Удалить пространство
                </button>
              ) : null}
            </div>
          </div>
        </main>
        <footer className="app-footer">
          <span>
            forma <span className="muted">/ AI WORKSPACE</span>
          </span>
          <span>Создано для ваших идей. Работает локально.</span>
        </footer>
      </div>
      <AssistantPanel />
      <Dialog.Root
        open={dialog !== null}
        onOpenChange={(v) => {
          if (!v) setDialog(null);
        }}
      >
        <Dialog.Portal>
          <Dialog.Overlay className="dialog-overlay" />
          <Dialog.Content className="dialog-content">
            <div className="dialog-top">
              <span className="dialog-icon">
                <Settings2 size={20} />
              </span>
              <Dialog.Close className="icon-button" aria-label="Закрыть">
                <X size={20} />
              </Dialog.Close>
            </div>
            <Dialog.Title>
              {dialog === "create"
                ? "Что создадим?"
                : dialog === "workspace"
                  ? "Новое пространство"
                  : dialog === "tab"
                    ? "Новая вкладка"
                    : dialog === "connect"
                      ? "Подключите своего AI"
                      : dialog === "delete"
                        ? "Удалить пространство?"
                        : "Очистить рабочую область?"}
            </Dialog.Title>
            <Dialog.Description>
              {dialog === "create"
                ? "Выберите инструмент. Результат сразу появится на панели."
                : dialog === "connect"
                  ? "Используйте STDIO или защищённый Streamable HTTP. Панель запускается вместе с сервером."
                  : dialog === "clear" || dialog === "delete"
                    ? "Это действие удалит сохранённые данные. Подтвердите его выполнение."
                    : "Дайте ему короткое и понятное название."}
            </Dialog.Description>
            {dialog === "create" ? (
              <>
                <div className="math-creator">
                  <div className="creator-title">
                    <Sigma size={19} />
                    <strong>Математическая задача</strong>
                  </div>
                  <div className="creator-selects">
                    <label>
                      Тема
                      <select
                        value={kind}
                        onChange={(e) => setKind(e.target.value as MathKind)}
                      >
                        {Object.entries({
                          quadratic: "Квадратные уравнения",
                          linear: "Линейные уравнения",
                          arithmetic: "Арифметика",
                          system: "Системы уравнений",
                          percent: "Проценты",
                          fraction: "Дроби",
                        }).map(([k, v]) => (
                          <option key={k} value={k}>
                            {v}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label>
                      Сложность
                      <select
                        value={difficulty}
                        onChange={(e) => setDifficulty(e.target.value)}
                      >
                        <option value="easy">Начальная</option>
                        <option value="medium">Средняя</option>
                        <option value="hard">Продвинутая</option>
                      </select>
                    </label>
                  </div>
                  <button
                    className="primary"
                    disabled={busy}
                    onClick={() =>
                      void action(() =>
                        command("math_create_problem", { kind, difficulty }),
                      )
                    }
                  >
                    Создать задачу
                    <ArrowRight size={16} />
                  </button>
                </div>
                <MediaUpload />
                <div className="widget-library">
                  {Object.entries({
                    checklist: ["Чек-лист", "Пункты с сохранением"],
                    metric: ["Показатель", "Число, единицы и динамика"],
                    progress: ["Прогресс", "Процент выполнения"],
                    chart: ["График функции", "line · bar · pie · scatter"],
                    table: ["Таблица", "Поиск и сортировка"],
                    markdown: ["Документ", "Безопасный Markdown"],
                    code: ["Код", "Подсветка и копирование"],
                    quiz: ["Тест", "5 вопросов с проверкой"],
                    notification: ["Сообщение", "Заметка от AI"],
                  }).map(([key, [title, description]]) => (
                    <button
                      key={key}
                      disabled={busy}
                      onClick={() =>
                        void action(samples[key as keyof typeof samples])
                      }
                    >
                      <span>{title}</span>
                      <small>{description}</small>
                      <Plus size={16} />
                    </button>
                  ))}
                </div>
              </>
            ) : null}
            {dialog === "workspace" || dialog === "tab" ? (
              <form
                onSubmit={(e) => {
                  e.preventDefault();
                  void action(async () => {
                    const res = await command(
                      dialog === "workspace"
                        ? "workspace_create"
                        : "ui_create_tab",
                      dialog === "workspace" ? { name } : { title: name },
                    );
                    if (dialog === "workspace" && res.workspace_id)
                      await selectWorkspace(res.workspace_id);
                  });
                }}
              >
                <label className="field-label" htmlFor="new-name">
                  Название
                </label>
                <input
                  id="new-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={
                    dialog === "workspace"
                      ? "Например, исследование"
                      : "Например, математика"
                  }
                  required
                  maxLength={100}
                />
                <button className="primary full-width" disabled={busy}>
                  Создать
                  <Plus size={16} />
                </button>
              </form>
            ) : null}
            {dialog === "connect" ? (
              <div className="connect-guide">
                <h3>STDIO · сервер запускает Codex</h3>
                <p>
                  Остановите этот экземпляр перед запуском на том же порту.
                  Укажите абсолютный путь к бинарнику.
                </p>
                <pre>
                  <code>
                    codex mcp add ai-workspace -- /absolute/path/ai-workspace
                    --transport stdio --data-dir /absolute/path/data
                  </code>
                </pre>
                <h3>HTTP · этот сервер уже запущен</h3>
                <pre>
                  <code>
                    export WORKSPACE_TOKEN="$(cat .ai-workspace/mcp-token)"
                    {"\n"}codex mcp add ai-workspace --url
                    http://127.0.0.1:8080/mcp --bearer-token-env-var
                    WORKSPACE_TOKEN
                  </code>
                </pre>
                <p>
                  Токен находится в файле mcp-token внутри каталога данных.
                  Подробности и примеры — в README.md.
                </p>
                <div className="connection-example">
                  «Создай математическую задачу средней сложности и покажи её на
                  панели»
                </div>
              </div>
            ) : null}
            {dialog === "clear" || dialog === "delete" ? (
              <div className="confirm-actions">
                <Dialog.Close className="quiet">Отмена</Dialog.Close>
                <button
                  className="primary destructive"
                  disabled={busy}
                  onClick={() =>
                    void action(async () => {
                      await command(
                        dialog === "delete"
                          ? "workspace_delete"
                          : "workspace_clear",
                      );
                      if (dialog === "delete") {
                        await selectWorkspace("default");
                        await refreshList();
                      }
                    })
                  }
                >
                  {dialog === "delete"
                    ? "Удалить пространство"
                    : "Удалить все виджеты"}
                </button>
              </div>
            ) : null}
          </Dialog.Content>
        </Dialog.Portal>
      </Dialog.Root>
    </div>
  );
}
