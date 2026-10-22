package rekey

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"path/filepath"
	"testing"

	"calendar/server/internal/db"
	"calendar/server/internal/vault"
)

func TestRekey(t *testing.T) {
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	oldV, _ := vault.New(bytes.Repeat([]byte{1}, 32))
	newV, _ := vault.New(bytes.Repeat([]byte{2}, 32))
	for _, id := range []string{"A1", "A2"} {
		s, _ := oldV.Seal([]byte(`{"refresh_token":"rt-`+id+`"}`), []byte(id))
		d.W.Exec(`INSERT INTO accounts(id,provider,external_id,email,color,status,secret,created_at) VALUES (?,?,?,?,?,?,?,1)`, id, "google", id, id+"@x", "cobalt", "ok", s)
	}
	sec, _ := oldV.Seal([]byte("hook-secret"), []byte("AU1"))
	act, _ := json.Marshal(map[string]any{"type": "webhook", "url": "http://x", "secretSealed": base64.StdEncoding.EncodeToString(sec)})
	d.W.Exec(`INSERT INTO automations(id,name,enabled,trigger,action) VALUES ('AU1','n',1,'{}',?)`, string(act))
	d.W.Exec(`INSERT INTO automations(id,name,enabled,trigger,action) VALUES ('AU2','plain',1,'{}','{"type":"ntfy","topic":"t"}')`)

	// the wrong old key changes nothing
	if _, err := Run(context.Background(), d, newV, newV); err == nil {
		t.Fatal("wrong old key accepted")
	}
	res, err := Run(context.Background(), d, oldV, newV)
	if err != nil || res.Accounts != 2 || res.Automations != 1 {
		t.Fatal(err, res)
	}
	for _, id := range []string{"A1", "A2"} {
		var s []byte
		d.R.QueryRow(`SELECT secret FROM accounts WHERE id=?`, id).Scan(&s)
		if pt, err := newV.Open(s, []byte(id)); err != nil || string(pt) != `{"refresh_token":"rt-`+id+`"}` {
			t.Fatal(id, err)
		}
		if _, err := oldV.Open(s, []byte(id)); err == nil {
			t.Fatal("old key still opens", id)
		}
	}
	var a string
	d.R.QueryRow(`SELECT action FROM automations WHERE id='AU1'`).Scan(&a)
	var m map[string]string
	json.Unmarshal([]byte(a), &m)
	raw, _ := base64.StdEncoding.DecodeString(m["secretSealed"])
	if pt, err := newV.Open(raw, []byte("AU1")); err != nil || string(pt) != "hook-secret" {
		t.Fatal(err)
	}
	// one unreadable account aborts everything
	d.W.Exec(`UPDATE accounts SET secret=x'00' WHERE id='A2'`)
	if _, err := Run(context.Background(), d, newV, oldV); err == nil {
		t.Fatal("damaged account tolerated")
	}
	var s1 []byte
	d.R.QueryRow(`SELECT secret FROM accounts WHERE id='A1'`).Scan(&s1)
	if _, err := newV.Open(s1, []byte("A1")); err != nil {
		t.Fatal("partial rekey left A1 re-sealed")
	}
}
