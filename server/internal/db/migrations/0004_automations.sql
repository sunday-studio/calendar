-- B3: automations bookkeeping. The `automations` table itself is from 0001.
CREATE TABLE automation_fired (                      -- an occurrence triggers an event.start automation once
  automation_id TEXT NOT NULL, event_id TEXT NOT NULL, rkey INTEGER NOT NULL, fired_at INTEGER NOT NULL,
  PRIMARY KEY (automation_id, event_id, rkey)
) WITHOUT ROWID;
CREATE TABLE automation_seen (event_id TEXT PRIMARY KEY);   -- events already announced as "created"
CREATE TABLE automation_runs (
  id INTEGER PRIMARY KEY, automation_id TEXT NOT NULL, ts INTEGER NOT NULL, trigger TEXT NOT NULL,
  ok INTEGER NOT NULL, status INTEGER, detail TEXT, ms INTEGER
);
CREATE INDEX automation_runs_a ON automation_runs(automation_id, id);
