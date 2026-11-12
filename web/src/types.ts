export type Difficulty = "easy" | "medium" | "hard";
export type MathKind =
  | "arithmetic"
  | "linear"
  | "quadratic"
  | "system"
  | "percent"
  | "fraction";
export type Problem = {
  kind: MathKind;
  difficulty: Difficulty;
  prompt: string;
  latex: string;
  hints: string[];
  steps?: string[];
  revealed: boolean;
  attempts: { answer: string; correct: boolean }[];
};
type Base = { id: string; title: string; tab_id: string; position: number };
export type Point = { x: number; y: number; label?: string };
export type ChartConfig = {
  kind: "line" | "bar" | "pie" | "scatter";
  points: Point[];
  x_label?: string;
  y_label?: string;
};
export type MediaImage = { url: string; alt: string; caption?: string };
export type MediaAsset = {
  id: string;
  name: string;
  mime: string;
  size: number;
  url: string;
};
export type Widget = Base &
  (
    | { type: "math_problem"; config: Record<string, never>; problem: Problem }
    | { type: "quiz"; config: Record<string, never>; questions: Problem[] }
    | { type: "chart"; config: ChartConfig }
    | { type: "markdown"; config: { content: string } }
    | { type: "image"; config: MediaImage }
    | { type: "gallery"; config: { images: MediaImage[] } }
    | { type: "video" | "audio"; config: { url: string; caption?: string } }
    | {
        type: "file";
        config: { url: string; filename: string; description?: string };
      }
    | {
        type: "checklist";
        config: { items: { id: string; text: string; done: boolean }[] };
      }
    | {
        type: "metric";
        config: {
          value: string;
          unit?: string;
          label?: string;
          change?: string;
          trend?: "up" | "down" | "neutral";
        };
      }
    | {
        type: "progress";
        config: { value: number; label?: string; detail?: string };
      }
    | { type: "code"; config: { code: string; language: string } }
    | { type: "table"; config: { columns: string[]; rows: string[][] } }
    | {
        type: "notification";
        config: {
          message: string;
          level: "info" | "success" | "warning" | "error";
        };
      }
  );
export type Workspace = {
  id: string;
  name: string;
  version: number;
  tabs: { id: string; title: string }[];
  active_tab: string;
  layout: "grid" | "list";
  theme: "system" | "dark" | "light";
  widgets: Widget[];
};
export type WorkspaceSummary = {
  id: string;
  name: string;
  version: number;
  widget_count: number;
};
export type ToolResult = {
  asset?: MediaAsset;
  state?: Workspace;
  widget?: Widget;
  correct?: boolean;
  workspace_id?: string;
  version?: number;
  steps?: string[];
};
export type WireEvent = {
  type: string;
  workspace_id?: string;
  version?: number;
  payload?: Workspace;
  error?: string;
};
