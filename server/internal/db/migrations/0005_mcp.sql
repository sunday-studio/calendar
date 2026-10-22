-- B5: hub MCP server. Audit log of tool calls and human-approval handles for mutating tools.
CREATE TABLE mcp_calls (
  id INTEGER PRIMARY KEY, ts INTEGER NOT NULL, token_id TEXT NOT NULL, tool TEXT NOT NULL,
  ok INTEGER NOT NULL, code TEXT, ms INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX mcp_calls_ts ON mcp_calls(ts);
CREATE TABLE mcp_confirmations (                     -- single-use, 10-minute approval handles
  hash BLOB PRIMARY KEY,                             -- sha256 of the confirm token (the token itself is never stored)
  id TEXT NOT NULL UNIQUE, kind TEXT NOT NULL,       -- create | update | delete | rsvp
  token_id TEXT NOT NULL,                            -- the API token that drafted it; only that token may execute it
  summary TEXT NOT NULL, payload TEXT NOT NULL,      -- JSON: what the human sees / what will run
  created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL,
  approved_at INTEGER, approved_by TEXT, used_at INTEGER, outcome TEXT
);
CREATE INDEX mcp_confirmations_exp ON mcp_confirmations(expires_at);
