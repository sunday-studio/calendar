// Package config reads settings from the environment; secrets may come from files.
package config

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Addr               string        // CALENDAR_ADDR, default 127.0.0.1:8787
	DBPath             string        // CALENDAR_DB, default ./calendar.db
	PublicURL          string        // CALENDAR_PUBLIC_URL
	MasterKey          []byte        // CALENDAR_MASTER_KEY_FILE or CALENDAR_MASTER_KEY; 32 bytes
	GoogleClientFile   string        // CALENDAR_GOOGLE_CLIENT_FILE: JSON {client_id, client_secret}; empty = Google disabled
	GoogleClientID     string        // CALENDAR_GOOGLE_CLIENT_ID + CALENDAR_GOOGLE_CLIENT_SECRET: the same client as values; the file wins
	GoogleClientSecret string        // CALENDAR_GOOGLE_CLIENT_SECRET
	SyncFast           time.Duration // CALENDAR_SYNC_FAST: poll cadence while a client is connected (default 60s)
	SyncSlow           time.Duration // CALENDAR_SYNC_SLOW: poll cadence otherwise (default 5m)
	APNsKeyFile        string        // CALENDAR_APNS_KEY_FILE: the .p8 auth key; empty = push disabled
	APNsKey            string        // CALENDAR_APNS_KEY: the same key as a value (PEM, PEM with \n escapes, or base64 of the PEM); the file wins
	APNsKeyID          string        // CALENDAR_APNS_KEY_ID
	APNsTeamID         string        // CALENDAR_APNS_TEAM_ID
	APNsTopic          string        // CALENDAR_APNS_TOPIC: bundle id (default studio.thesunday.calendar)
	BackupDir          string        // CALENDAR_BACKUP_DIR: nightly backups go here; empty = no scheduled backup
	BackupKeep         int           // CALENDAR_BACKUP_KEEP: how many to keep (default 14)
	BackupHour         int           // CALENDAR_BACKUP_HOUR: local hour of the nightly backup (default 3)
	BackupPlain        bool          // CALENDAR_BACKUP_PLAIN=1: do not encrypt backups
	AlfatherURL        string        // CALENDAR_ALFATHER_URL: alfather MCP endpoint; empty = alfather off
	AlfatherParseTool  string        // CALENDAR_ALFATHER_PARSE_TOOL (default parse_event)
	AlfatherToken      string        // CALENDAR_ALFATHER_TOKEN: optional bearer token for alfather
	ICSAllowPrivate    bool          // CALENDAR_ICS_ALLOW_PRIVATE=1: let ICS feeds live on loopback/private addresses
	ICSPoll            time.Duration // CALENDAR_ICS_POLL: ICS feed poll cadence (default 15m)
	MCPDisabled        bool          // CALENDAR_MCP_DISABLE=1: do not serve POST /mcp
	MCPRate            int           // CALENDAR_MCP_RATE: /mcp calls per token per minute (default 120)
	MCPMaxBody         int           // CALENDAR_MCP_MAX_BODY: bytes accepted on /mcp (default 262144)
	AuthFailLimit      int           // CALENDAR_AUTH_FAIL_LIMIT: invalid-token attempts per client per window (default 20)
	AuthFailWindow     time.Duration // CALENDAR_AUTH_FAIL_WINDOW (default 1m)
	// Google endpoint overrides (end-to-end runs against cmd/fakegoogle; empty = the real Google).
	GoogleAuthURL     string // CALENDAR_GOOGLE_AUTH_URL
	GoogleTokenURL    string // CALENDAR_GOOGLE_TOKEN_URL
	GoogleAPIBase     string // CALENDAR_GOOGLE_API_BASE: replaces https://www.googleapis.com/calendar/v3
	GoogleUserinfoURL string // CALENDAR_GOOGLE_USERINFO_URL
	Dev               bool   // CALENDAR_DEV=1: development helpers (calendard dev ...), a placeholder Google client when none is configured
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("%s: want a positive duration like 60s", k)
	}
	return d, nil
}

func envInt(k string, def, lo, hi int) (int, error) {
	v := os.Getenv(k)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, fmt.Errorf("%s: want a whole number from %d to %d", k, lo, hi)
	}
	return n, nil
}

// Load reads configuration. It does not require the master key (see RequireMasterKey).
func Load() (*Config, error) {
	c := &Config{
		Addr:               env("CALENDAR_ADDR", "127.0.0.1:8787"),
		DBPath:             env("CALENDAR_DB", "./calendar.db"),
		PublicURL:          env("CALENDAR_PUBLIC_URL", ""),
		GoogleClientFile:   os.Getenv("CALENDAR_GOOGLE_CLIENT_FILE"),
		APNsKeyFile:        os.Getenv("CALENDAR_APNS_KEY_FILE"),
		APNsKey:            os.Getenv("CALENDAR_APNS_KEY"),
		GoogleClientID:     os.Getenv("CALENDAR_GOOGLE_CLIENT_ID"),
		GoogleClientSecret: os.Getenv("CALENDAR_GOOGLE_CLIENT_SECRET"),
		APNsKeyID:          os.Getenv("CALENDAR_APNS_KEY_ID"),
		APNsTeamID:         os.Getenv("CALENDAR_APNS_TEAM_ID"),
		APNsTopic:          env("CALENDAR_APNS_TOPIC", "studio.thesunday.calendar"),
		AlfatherURL:        os.Getenv("CALENDAR_ALFATHER_URL"),
		AlfatherParseTool:  env("CALENDAR_ALFATHER_PARSE_TOOL", "parse_event"),
		AlfatherToken:      os.Getenv("CALENDAR_ALFATHER_TOKEN"),
		MCPDisabled:        os.Getenv("CALENDAR_MCP_DISABLE") == "1",
		GoogleAuthURL:      os.Getenv("CALENDAR_GOOGLE_AUTH_URL"),
		GoogleTokenURL:     os.Getenv("CALENDAR_GOOGLE_TOKEN_URL"),
		GoogleAPIBase:      strings.TrimRight(os.Getenv("CALENDAR_GOOGLE_API_BASE"), "/"),
		GoogleUserinfoURL:  os.Getenv("CALENDAR_GOOGLE_USERINFO_URL"),
		Dev:                os.Getenv("CALENDAR_DEV") == "1",
	}
	var err error
	if c.BackupKeep, err = envInt("CALENDAR_BACKUP_KEEP", 14, 1, 3650); err != nil {
		return nil, err
	}
	if c.BackupHour, err = envInt("CALENDAR_BACKUP_HOUR", 3, 0, 23); err != nil {
		return nil, err
	}
	if c.ICSPoll, err = envDur("CALENDAR_ICS_POLL", 15*time.Minute); err != nil {
		return nil, err
	}
	c.ICSAllowPrivate = os.Getenv("CALENDAR_ICS_ALLOW_PRIVATE") == "1"
	if c.MCPRate, err = envInt("CALENDAR_MCP_RATE", 120, 1, 100000); err != nil {
		return nil, err
	}
	if c.MCPMaxBody, err = envInt("CALENDAR_MCP_MAX_BODY", 256<<10, 1024, 64<<20); err != nil {
		return nil, err
	}
	if c.AuthFailLimit, err = envInt("CALENDAR_AUTH_FAIL_LIMIT", 20, 1, 100000); err != nil {
		return nil, err
	}
	if c.AuthFailWindow, err = envDur("CALENDAR_AUTH_FAIL_WINDOW", time.Minute); err != nil {
		return nil, err
	}
	c.BackupDir = os.Getenv("CALENDAR_BACKUP_DIR")
	c.BackupPlain = os.Getenv("CALENDAR_BACKUP_PLAIN") == "1"
	if c.SyncFast, err = envDur("CALENDAR_SYNC_FAST", time.Minute); err != nil {
		return nil, err
	}
	if c.SyncSlow, err = envDur("CALENDAR_SYNC_SLOW", 5*time.Minute); err != nil {
		return nil, err
	}
	raw := os.Getenv("CALENDAR_MASTER_KEY")
	if f := os.Getenv("CALENDAR_MASTER_KEY_FILE"); f != "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read master key file: %w", err)
		}
		raw = string(b)
	}
	if raw != "" {
		k, err := ParseKey(raw)
		if err != nil {
			return nil, err
		}
		c.MasterKey = k
	}
	return c, nil
}

// RequireMasterKey is called by `serve`; it refuses to proceed without a valid key.
func (c *Config) RequireMasterKey() error {
	if len(c.MasterKey) != 32 {
		return errors.New("master key missing: set CALENDAR_MASTER_KEY_FILE or CALENDAR_MASTER_KEY (32 bytes, base64 or hex)")
	}
	return nil
}

// ParseKey accepts 32 raw bytes, or base64 / hex that decodes to 32 bytes.
func ParseKey(s string) ([]byte, error) {
	t := strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(t); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(t); err == nil && len(b) == 32 {
		return b, nil
	}
	if b := bytes.TrimRight([]byte(s), "\r\n"); len(b) == 32 {
		return b, nil
	}
	return nil, errors.New("master key must be 32 bytes (raw, base64 or hex)")
}
