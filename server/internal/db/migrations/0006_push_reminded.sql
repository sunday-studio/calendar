-- all-day reminder pushes already sent (one per occurrence)
CREATE TABLE push_reminded (
  event_id TEXT NOT NULL, rkey INTEGER NOT NULL, sent_at INTEGER NOT NULL,
  PRIMARY KEY (event_id, rkey)
) WITHOUT ROWID;
