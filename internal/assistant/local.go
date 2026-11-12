package assistant

import (
	"aiworkspace/internal/workspace"
	"context"
	"time"
)

// LocalSession gives the Codex adapter the same schema/receipt/confirmation router.
// It does not create a Realtime call or require an API key.
func (m *Manager) LocalSession(ctx context.Context, owner, id string) (string, error) {
	if _, e := m.service.Snapshot(id); e != nil {
		return "", e
	}
	if e := ctx.Err(); e != nil {
		return "", e
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil {
		return "", ErrSession
	}
	sessionCtx, cancel := context.WithCancel(m.ctx)
	sess := &ownedSession{Session: Session{ID: workspace.ID(), WorkspaceID: id, Expires: time.Now().Add(30 * time.Minute), Cancel: cancel}, owner: owner, ctx: sessionCtx}
	m.sessions[sess.ID] = sess
	return sess.ID, nil
}
