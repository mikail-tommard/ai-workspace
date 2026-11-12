package workspace

import "aiworkspace/internal/mathengine"

type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}
type Widget struct {
	ID        string               `json:"id"`
	Type      string               `json:"type"`
	Title     string               `json:"title"`
	TabID     string               `json:"tab_id"`
	Position  int                  `json:"position"`
	Config    map[string]any       `json:"config"`
	Problem   *mathengine.Problem  `json:"problem,omitempty"`
	Questions []mathengine.Problem `json:"questions,omitempty"`
}
type State struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Version   int64    `json:"version"`
	Tabs      []Tab    `json:"tabs"`
	ActiveTab string   `json:"active_tab"`
	Layout    string   `json:"layout"`
	Theme     string   `json:"theme"`
	Widgets   []Widget `json:"widgets"`
}
type Event struct {
	Type        string `json:"type"`
	WorkspaceID string `json:"workspace_id"`
	WidgetID    string `json:"widget_id,omitempty"`
	Version     int64  `json:"version"`
	Payload     any    `json:"payload"`
}
type Result map[string]any
