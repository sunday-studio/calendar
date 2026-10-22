-- B2: push. changes.src marks rows written by the user's own request (never alerted back at them);
-- push_state remembers each event's next start as last pushed, to tell "moved" from "edited".
ALTER TABLE changes ADD COLUMN src TEXT NOT NULL DEFAULT '';
ALTER TABLE push_endpoints ADD COLUMN bundle_id TEXT;
CREATE TABLE push_state (
  event_id TEXT PRIMARY KEY, start_utc INTEGER NOT NULL, status TEXT NOT NULL, seen_at INTEGER NOT NULL
);
