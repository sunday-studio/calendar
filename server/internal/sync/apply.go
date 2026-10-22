package sync

import (
	"context"
	"time"

	"calendar/server/internal/db"
	"calendar/server/internal/domain"
)

// Apply writes what a provider confirmed for a user write into the mirror, inside the
// caller's transaction (same code path as a sync page, minus the cursor). Events with
// Status Cancelled and ids in deleted become tombstones; affected series are re-materialised.
func Apply(ctx context.Context, tx *db.Tx, mat Materialiser, cal domain.Calendar, events []domain.Event, deleted []string, now time.Time) error {
	_, err := applyBatch(ctx, tx, mat, cal, events, deleted, nil, false, "", now)
	return err
}

// Materialiser returns the read-model builder the manager uses (the default when none is set).
func (m *Manager) Materialiser() Materialiser { return m.mat() }

// Nudge asks the account's worker for an after-write sync, so remote side effects (a generated
// Meet link, server-side defaults) reach the mirror. Coalesces with any pending trigger.
func (m *Manager) Nudge(accountID string) {
	if w := m.ensureWorker(accountID); w != nil {
		m.send(w, AfterWrite)
	}
}
