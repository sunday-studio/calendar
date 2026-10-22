CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);       -- rev, schema_version, instance_id

CREATE TABLE tokens (                                                 -- device + script credentials
  id TEXT PRIMARY KEY,                                                -- ulid
  name TEXT NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('device','script')),
  scope TEXT NOT NULL CHECK (scope IN ('read','write','admin')),
  hash BLOB NOT NULL UNIQUE,                                          -- sha256(secret); secret = "cal_" + 32 random bytes b32
  prefix TEXT NOT NULL,                                               -- shown in UI, e.g. "cal_7QK2"
  created_at INTEGER NOT NULL, last_used_at INTEGER, revoked_at INTEGER
);

CREATE TABLE push_endpoints (                                         -- APNs targets, per device token
  token_id TEXT NOT NULL REFERENCES tokens(id) ON DELETE CASCADE,
  apns_token TEXT NOT NULL, env TEXT NOT NULL CHECK (env IN ('sandbox','production')),
  live_activity_start_token TEXT, updated_at INTEGER NOT NULL,
  PRIMARY KEY (token_id, apns_token)
);

CREATE TABLE accounts (
  id TEXT PRIMARY KEY,
  provider TEXT NOT NULL CHECK (provider IN ('google','microsoft','icloud','ics')),
  external_id TEXT NOT NULL,                                          -- google sub / ms oid / apple id / sha256(ics url)
  email TEXT NOT NULL, display_name TEXT,
  color TEXT NOT NULL,                                                -- palette key: cobalt|rose|ochre|mauve|verdigris|slate|moss|clay
  status TEXT NOT NULL CHECK (status IN ('connecting','ok','needs_reauth','error','disabled')),
  status_detail TEXT,
  secret BLOB NOT NULL,                                               -- vault-sealed JSON (refresh token / app password / ics url)
  granted_scopes TEXT,
  last_sync_at INTEGER, last_ok_at INTEGER, next_sync_at INTEGER, fail_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  UNIQUE (provider, external_id)                                      -- connecting twice updates, never duplicates (US-1.1)
);

CREATE TABLE calendars (
  id TEXT PRIMARY KEY,
  account_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
  remote_id TEXT NOT NULL, name TEXT NOT NULL,
  remote_color TEXT, color_override TEXT,                             -- US-4.2
  tzid TEXT,
  access TEXT NOT NULL CHECK (access IN ('owner','writer','reader','freebusy')),
  is_primary INTEGER NOT NULL DEFAULT 0,
  visible INTEGER NOT NULL DEFAULT 1,                                 -- US-4.1
  sync_cursor TEXT,                                                   -- google syncToken | graph deltaLink | caldav sync-token/ctag | ics etag
  window_from INTEGER, window_to INTEGER,                             -- graph delta window
  last_sync_at INTEGER,
  UNIQUE (account_id, remote_id)
);

CREATE TABLE events (
  id TEXT PRIMARY KEY,
  calendar_id TEXT NOT NULL REFERENCES calendars(id) ON DELETE CASCADE,
  remote_id TEXT NOT NULL, ical_uid TEXT,
  kind TEXT NOT NULL CHECK (kind IN ('single','master','exception','instance')),
  series_id TEXT REFERENCES events(id) ON DELETE SET NULL,            -- exception/instance → master (if mirrored)
  remote_series_id TEXT,
  recurrence_key INTEGER,                                             -- original start (unix s) this exception overrides
  title TEXT NOT NULL DEFAULT '', description TEXT, location TEXT,
  all_day INTEGER NOT NULL DEFAULT 0,
  start_utc INTEGER, end_utc INTEGER, tzid TEXT,                      -- timed
  start_date TEXT, end_date TEXT,                                     -- all-day, 'YYYY-MM-DD', end exclusive
  rrule TEXT, rdates TEXT, exdates TEXT,                              -- masters only (JSON arrays for the last two)
  status TEXT NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed','tentative','cancelled')),
  busy INTEGER NOT NULL DEFAULT 1,
  organizer_email TEXT, organizer_name TEXT, is_organizer INTEGER NOT NULL DEFAULT 0,
  self_response TEXT CHECK (self_response IN ('needsAction','accepted','declined','tentative')),
  conf_url TEXT, conf_kind TEXT CHECK (conf_kind IN ('meet','teams','zoom','webex','other')),
  conf_source TEXT CHECK (conf_source IN ('structured','parsed')),
  html_link TEXT,
  etag TEXT, remote_updated_at INTEGER,
  can_edit INTEGER NOT NULL DEFAULT 0, can_rsvp INTEGER NOT NULL DEFAULT 0, can_add_conf INTEGER NOT NULL DEFAULT 0,
  rev INTEGER NOT NULL,                                               -- global rev at last change = the `version` clients send in If-Match
  deleted_at INTEGER,
  UNIQUE (calendar_id, remote_id)
);
CREATE INDEX events_series ON events(series_id);
CREATE INDEX events_uid    ON events(ical_uid);

CREATE TABLE attendees (
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  email TEXT NOT NULL, name TEXT,
  response TEXT NOT NULL DEFAULT 'needsAction', role TEXT NOT NULL DEFAULT 'required',   -- required|optional|resource
  is_self INTEGER NOT NULL DEFAULT 0, is_organizer INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (event_id, email)
);

CREATE TABLE occurrences (                                            -- READ MODEL
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  rkey INTEGER NOT NULL,                                              -- recurrence key, or 0 for non-recurring
  calendar_id TEXT NOT NULL,                                          -- denormalised for fast visible-calendar filters
  all_day INTEGER NOT NULL,
  start_utc INTEGER NOT NULL, end_utc INTEGER NOT NULL,               -- for all-day: instants in the *user's* tz at materialise time, for ordering only
  start_date TEXT, end_date TEXT,                                     -- authoritative for all-day
  PRIMARY KEY (event_id, rkey)
) WITHOUT ROWID;
CREATE INDEX occ_range ON occurrences(start_utc, end_utc);
CREATE INDEX occ_cal   ON occurrences(calendar_id, start_utc);

CREATE TABLE reminder_overrides (                                     -- US-6.2 per-event override, shared by every device
  event_id TEXT NOT NULL REFERENCES events(id) ON DELETE CASCADE,
  rkey INTEGER NOT NULL DEFAULT 0, minutes_before INTEGER,            -- NULL = no reminder
  PRIMARY KEY (event_id, rkey)
);

CREATE TABLE changes (                                                -- rev log; SSE + audit
  rev INTEGER PRIMARY KEY AUTOINCREMENT,
  entity TEXT NOT NULL, entity_id TEXT NOT NULL, op TEXT NOT NULL,    -- upsert|delete|status
  ts INTEGER NOT NULL
);

CREATE TABLE idempotency (                                            -- write dedupe, 24h TTL
  key TEXT NOT NULL, token_id TEXT NOT NULL, req_hash BLOB NOT NULL,
  status INTEGER NOT NULL, response BLOB NOT NULL, created_at INTEGER NOT NULL,
  PRIMARY KEY (token_id, key)
);

CREATE TABLE oauth_sessions (                                         -- in-flight "Connect Google/Microsoft"
  state TEXT PRIMARY KEY, provider TEXT NOT NULL, pkce_verifier TEXT NOT NULL,
  token_id TEXT NOT NULL, reauth_account_id TEXT, expires_at INTEGER NOT NULL
);

CREATE TABLE sync_runs (
  id INTEGER PRIMARY KEY, account_id TEXT NOT NULL, calendar_id TEXT,
  started_at INTEGER NOT NULL, duration_ms INTEGER, trigger TEXT,
  added INTEGER, changed INTEGER, removed INTEGER, error_kind TEXT, error TEXT
);

CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);    -- JSON: default_calendar, working_hours, week_start, secondary_tz, alert_defaults, user_tz

CREATE TABLE automations (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1,
  trigger TEXT NOT NULL,        -- {"type":"event.start","offset_min":-5,"calendar":"…"} | {"type":"cron","expr":"0 7 * * *"} | {"type":"event.created"}
  action  TEXT NOT NULL         -- {"type":"webhook","url":"…","secret":"…"} | {"type":"ntfy","topic":"…"}
);

CREATE VIRTUAL TABLE events_fts USING fts5(title, location, description, attendees, content='', tokenize='unicode61 remove_diacritics 2');

INSERT INTO meta(key, value) VALUES ('rev', '0'), ('schema_version', '1');
