package workspace

import "sort"

type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema *Schema         `json:"inputSchema"`
	Annotations map[string]bool `json:"annotations"`
}

func Tools(reg Registry) map[string]Tool {
	out := map[string]Tool{}
	add := func(name, description string, props map[string]*Schema, required ...string) {
		read := name == "workspace_list" || name == "workspace_get" || name == "workspace_get_state" || name == "widget_list" || name == "widget_get"
		out[name] = Tool{name, description, Obj(props, required...), map[string]bool{"readOnlyHint": read, "destructiveHint": name == "workspace_clear" || name == "workspace_delete" || name == "widget_delete" || name == "chart_delete", "openWorldHint": false}}
	}
	ws := func() map[string]*Schema {
		return map[string]*Schema{"workspace_id": Str("Workspace ID. Omit to use default.", 100)}
	}
	target := func() map[string]*Schema {
		p := ws()
		p["widget_id"] = Str("Existing widget ID returned by creation tools", 100)
		return p
	}
	creation := func() map[string]*Schema {
		p := ws()
		p["title"] = Str("Human-readable card title", 200)
		p["tab_id"] = Str("Tab ID; defaults to active tab", 100)
		return p
	}
	add("workspace_create", "Create a persistent workspace with its own tabs and widgets.", map[string]*Schema{"name": Str("Workspace name", 100)}, "name")
	add("workspace_list", "List accessible local workspaces and their versions.", map[string]*Schema{})
	for _, name := range []string{"workspace_get", "workspace_get_state"} {
		add(name, "Read complete current workspace state, including widget IDs, tabs and state version.", ws())
	}
	add("workspace_clear", "Remove all widgets from a workspace, retaining its tabs.", ws())
	add("workspace_delete", "Delete an active workspace. Audit history remains locally; the default workspace cannot be deleted.", ws())
	p := creation()
	p["path"] = Str("Absolute local media path. Generated Codex images are supported; imports are restricted to configured roots. Do NOT use file:// URLs.", 4096)
	p["alt"] = Str("Accessible image description", 500)
	p["attach"] = &Schema{Type: "boolean", Description: "Default true: import AND display a widget. False imports only and returns an asset URL, useful for a gallery."}
	add("media_import", "Import a local image/photo, audio/video or PDF into permanent Forma storage and display it on the panel. Use immediately after generating an image, including files in ~/.codex/generated_images. Returns asset URL and widget; no public HTTPS upload is required.", p, "path")
	p = creation()
	types := []string{}
	for kind := range reg {
		if kind != "math_problem" && kind != "quiz" {
			types = append(types, kind)
		}
	}
	sort.Strings(types)
	p["type"] = Enum(types...)
	choices := []*Schema{}
	for _, kind := range types {
		choices = append(choices, reg[kind])
	}
	configs := &Schema{AnyOf: choices, Description: "Typed widget configuration; must match selected widget type. image/gallery/audio/video/file accept managed local asset URLs returned by media_import."}

	p["config"] = configs
	add("widget_create", "Create a typed visual card. For tables use type=table and config={columns:[string], rows:[[string]]}. For math use math_create_problem. For local files call media_import; it imports AND displays images, audio, video or PDF. More widget types: gallery, checklist, metric, progress, audio, video, file.", p, "type", "title", "config")
	p = target()
	p["title"] = Str("New card title", 200)
	p["config"] = configs
	p["math"] = Obj(map[string]*Schema{"kind": Enum("arithmetic", "linear", "quadratic", "system", "percent", "fraction"), "difficulty": Enum("easy", "medium", "hard")})
	add("widget_update", "Update title/config, or regenerate a math_problem in place using math={difficulty:hard,kind:quadratic}. Math updates preserve widget ID, tab and position and reset attempts/solution.", p, "widget_id")
	add("widget_delete", "Delete one widget by ID.", target(), "widget_id")
	add("widget_list", "List all widgets in the workspace.", ws())
	add("widget_get", "Read a widget and its current interaction state.", target(), "widget_id")
	p = target()
	p["tab_id"] = Str("Destination tab ID", 100)
	p["position"] = Integer(0, 199)
	add("widget_move", "Move a widget to another tab and/or change its display order.", p, "widget_id", "position")
	p = creation()
	p["kind"] = Enum("arithmetic", "linear", "quadratic", "system", "percent", "fraction")
	p["difficulty"] = Enum("easy", "medium", "hard")
	add("math_create_problem", "Generate and display a mathematically verified interactive problem. Defaults: medium quadratic. Supports arithmetic, linear/quadratic equations, systems, percentages and fractions.", p)
	p = target()
	p["answer"] = Str("Number, decimal or fraction; multiple roots separated by semicolon; systems: x=…; y=…", 512)
	p["question_index"] = Integer(0, 19)
	add("math_check_answer", "Check a user answer with exact rational arithmetic, save the attempt and update the card. For quiz specify question_index.", p, "widget_id", "answer")
	p = target()
	p["question_index"] = Integer(0, 19)
	add("math_show_solution", "Reveal and persist a verified step-by-step solution on a problem or a quiz question.", p, "widget_id")
	p = creation()
	p["count"] = Integer(1, 20)
	p["kind"] = Enum("arithmetic", "linear", "quadratic", "system", "percent", "fraction")
	p["difficulty"] = Enum("easy", "medium", "hard")
	add("math_generate_quiz", "Generate a quiz with independently checkable math questions. Defaults: 5 medium quadratic questions.", p)
	p = creation()
	p["config"] = reg["chart"]
	add("chart_create", "Display an interactive line, bar, pie or scatter chart from numeric x/y points. To plot a function, sample its points before calling.", p, "title", "config")
	p = target()
	p["config"] = reg["chart"]
	p["title"] = Str("New chart title", 200)
	add("chart_update", "Replace chart data and optionally title.", p, "widget_id", "config")
	add("chart_delete", "Delete a chart widget.", target(), "widget_id")
	for _, name := range []string{"text_create", "markdown_render"} {
		p = creation()
		p["content"] = Str("Safe Markdown content; raw HTML is never executed", 100000)
		add(name, "Create a safe formatted Markdown document card on the panel.", p, "title", "content")
	}
	p = target()
	p["content"] = Str("New safe Markdown content", 100000)
	add("text_update", "Replace the content of an existing Markdown card.", p, "widget_id", "content")
	p = creation()
	p["code"] = reg["code"].Properties["code"]
	p["language"] = reg["code"].Properties["language"]
	add("code_create", "Display syntax-highlighted source code with copy action; never execute the code.", p, "title", "code", "language")
	p = creation()
	p["message"] = reg["notification"].Properties["message"]
	p["level"] = reg["notification"].Properties["level"]
	add("ui_show_notification", "Show a persistent AI message card and request a quiet native macOS Notification Center banner on the server Mac. Use this to notify the local user after completing a task. Level: info, success, warning or error. OS delivery may fail independently of card creation.", p, "message")
	p = ws()
	p["title"] = Str("Tab title", 100)
	add("ui_create_tab", "Add and activate a new workspace tab.", p, "title")
	p = ws()
	p["tab_id"] = Str("Existing tab ID", 100)
	add("ui_switch_tab", "Switch the active tab for all browsers in this workspace.", p, "tab_id")
	p = ws()
	p["layout"] = Enum("grid", "list")
	p["theme"] = Enum("light", "dark", "system")
	add("ui_set_layout", "Persist grid/list layout and optionally light/dark/system appearance across browsers.", p)
	return out
}
func SortedTools(m map[string]Tool) []Tool {
	out := make([]Tool, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
