package push

import (
	"encoding/base64"
	"strings"
	"testing"
)

// A hosting panel's env var box holds the .p8 as a value: pasted as is, as one line with \n, or as base64.
func TestLoadAPNsValueTakesThePEMInEveryShapeAPanelStoresIt(t *testing.T) {
	_, pemBytes := testKey(t)
	multi := string(pemBytes)
	for name, v := range map[string]string{
		"as is":         multi,
		"padded":        "\n  " + multi + "\n",
		"escaped lines": strings.ReplaceAll(strings.TrimSpace(multi), "\n", `\n`),
		"base64":        base64.StdEncoding.EncodeToString(pemBytes),
	} {
		if _, err := LoadAPNsValue(v, "KEYID12345", "TEAMID1234", "dev.casprine.calendar"); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := LoadAPNsValue("not a key", "K", "T", "t"); err == nil {
		t.Error("garbage must be refused")
	}
}
