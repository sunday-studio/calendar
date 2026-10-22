package config

import (
	"testing"
	"time"
)

func TestAbuseLimitEnv(t *testing.T) {
	c, err := Load()
	if err != nil || c.MCPRate != 120 || c.MCPMaxBody != 256<<10 || c.AuthFailLimit != 20 || c.AuthFailWindow != time.Minute {
		t.Fatalf("defaults: %+v %v", c, err)
	}
	t.Setenv("CALENDAR_MCP_RATE", "30")
	t.Setenv("CALENDAR_MCP_MAX_BODY", "4096")
	t.Setenv("CALENDAR_AUTH_FAIL_LIMIT", "5")
	t.Setenv("CALENDAR_AUTH_FAIL_WINDOW", "30s")
	c, err = Load()
	if err != nil || c.MCPRate != 30 || c.MCPMaxBody != 4096 || c.AuthFailLimit != 5 || c.AuthFailWindow != 30*time.Second {
		t.Fatalf("%+v %v", c, err)
	}
	for k, v := range map[string]string{"CALENDAR_MCP_RATE": "0", "CALENDAR_MCP_MAX_BODY": "10", "CALENDAR_AUTH_FAIL_LIMIT": "x", "CALENDAR_AUTH_FAIL_WINDOW": "-1s"} {
		t.Setenv(k, v)
		if _, err := Load(); err == nil {
			t.Errorf("%s=%s accepted", k, v)
		}
		t.Setenv(k, map[string]string{"CALENDAR_MCP_RATE": "30", "CALENDAR_MCP_MAX_BODY": "4096", "CALENDAR_AUTH_FAIL_LIMIT": "5", "CALENDAR_AUTH_FAIL_WINDOW": "30s"}[k])
	}
}
