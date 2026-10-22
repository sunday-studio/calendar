package api

import (
	"context"
	"database/sql"
	"net/http"
	"strconv"
	"time"
)

func tsPtr(n sql.NullInt64) *time.Time {
	if !n.Valid {
		return nil
	}
	t := time.Unix(n.Int64, 0).UTC()
	return &t
}

func strPtr(n sql.NullString) *string {
	if !n.Valid || n.String == "" {
		return nil
	}
	return &n.String
}

func (s *Server) internalErr(w http.ResponseWriter, what string, err error) {
	s.Log.Error(what, "err", err)
	WriteError(w, http.StatusInternalServerError, "internal", "internal error", "")
}

// status implements GET /v1/status.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := s.Auth.DB
	st := Status{HubVersion: s.Version, Accounts: []AccountSyncStatus{}, Warnings: []Warning{}}
	var err error
	if st.Rev, err = d.Rev(ctx); err != nil {
		s.internalErr(w, "status rev", err)
		return
	}
	if st.DBSize, err = d.SizeBytes(ctx); err != nil {
		s.internalErr(w, "status size", err)
		return
	}
	if s.Stream != nil {
		st.SSEClients = s.Stream.Clients()
	}
	rows, err := d.R.QueryContext(ctx, `SELECT id, email, status, status_detail, last_sync_at, last_ok_at, next_sync_at, fail_count FROM accounts ORDER BY created_at, id`)
	if err != nil {
		s.internalErr(w, "status accounts", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var a AccountSyncStatus
		var detail sql.NullString
		var ls, lo, ns sql.NullInt64
		if err := rows.Scan(&a.AccountID, &a.Email, &a.Status, &detail, &ls, &lo, &ns, &a.FailCount); err != nil {
			s.internalErr(w, "status scan", err)
			return
		}
		a.Status = apiStatus(a.Status)
		a.LastError, a.LastSyncAt, a.LastOKAt, a.NextSyncAt = strPtr(detail), tsPtr(ls), tsPtr(lo), tsPtr(ns)
		if a.Status == "active" || a.Status == "syncing" {
			a.LastError = nil // detail is only meaningful while unhealthy
		}
		st.Accounts = append(st.Accounts, a)
	}
	if err := rows.Err(); err != nil {
		s.internalErr(w, "status rows", err)
		return
	}
	st.Warnings = append(st.Warnings, s.backupWarnings(ctx)...)
	if s.MCP != nil {
		if ms, err := s.MCP.Stats(ctx); err == nil {
			st.MCP = &ms
		}
	}
	WriteJSON(w, http.StatusOK, st)
}

// backupWarnings reports a stale or missing backup (only when scheduled backups are configured).
func (s *Server) backupWarnings(ctx context.Context) []Warning {
	if s.BackupDir == "" {
		return nil
	}
	const maxAge = 36 * time.Hour
	var v string
	err := s.Auth.DB.R.QueryRowContext(ctx, `SELECT value FROM meta WHERE key='last_backup_at'`).Scan(&v)
	if err == nil {
		if n, _ := strconv.ParseInt(v, 10, 64); n > 0 && time.Since(time.Unix(n, 0)) > maxAge {
			return []Warning{{Kind: "backup", Message: "The last backup is " + strconv.Itoa(int(time.Since(time.Unix(n, 0)).Hours())) + " hours old."}}
		}
		return nil
	}
	var first sql.NullInt64
	_ = s.Auth.DB.R.QueryRowContext(ctx, `SELECT MIN(created_at) FROM tokens`).Scan(&first)
	if first.Valid && time.Since(time.Unix(first.Int64, 0)) > maxAge {
		return []Warning{{Kind: "backup", Message: "No backup has been made yet."}}
	}
	return nil
}

// syncRuns implements GET /v1/sync/runs?limit=&accountId=.
func (s *Server) syncRuns(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			WriteError(w, http.StatusBadRequest, CodeValidation, "limit must be 1..200", "limit")
			return
		}
		limit = n
	}
	q := `SELECT id, account_id, calendar_id, started_at, COALESCE(duration_ms,0), COALESCE(trigger,''), COALESCE(added,0), COALESCE(changed,0), COALESCE(removed,0), error_kind, error FROM sync_runs`
	var args []any
	if a := r.URL.Query().Get("accountId"); a != "" {
		q += ` WHERE account_id=?`
		args = append(args, a)
	}
	q += ` ORDER BY started_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.Auth.DB.R.QueryContext(r.Context(), q, args...)
	if err != nil {
		s.internalErr(w, "sync runs", err)
		return
	}
	defer rows.Close()
	items := []SyncRun{}
	for rows.Next() {
		var x SyncRun
		var cal, ek, es sql.NullString
		var started int64
		if err := rows.Scan(&x.ID, &x.AccountID, &cal, &started, &x.DurationMs, &x.Trigger, &x.Added, &x.Changed, &x.Removed, &ek, &es); err != nil {
			s.internalErr(w, "sync runs scan", err)
			return
		}
		x.StartedAt = time.Unix(started, 0).UTC()
		x.CalendarID, x.ErrorKind, x.Error = strPtr(cal), strPtr(ek), strPtr(es)
		items = append(items, x)
	}
	if err := rows.Err(); err != nil {
		s.internalErr(w, "sync runs rows", err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}
