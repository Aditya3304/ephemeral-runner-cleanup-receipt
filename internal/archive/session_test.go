package archive

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAWSSessionExpiresAndRotates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	p := sessionProvider{path: path}
	write := func(key string, expiry time.Time) {
		t.Helper()
		b, _ := json.Marshal(map[string]any{"AccessKeyId": key, "SecretAccessKey": "secret", "SessionToken": "session", "Expiration": expiry})
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("first", time.Now().Add(time.Hour))
	v, e := p.Retrieve()
	if e != nil || v.AccessKeyID != "first" {
		t.Fatal("valid session rejected", e)
	}
	write("second", time.Now().Add(time.Hour))
	v, e = p.Retrieve()
	if e != nil || v.AccessKeyID != "second" {
		t.Fatal("rotation missed", e)
	}
	write("expired", time.Now().Add(-time.Second))
	if _, e = p.Retrieve(); e == nil {
		t.Fatal("expired credentials accepted")
	}
	if e = os.Remove(path); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink("missing", path); e != nil {
		t.Fatal(e)
	}
	if _, e = p.Retrieve(); e == nil {
		t.Fatal("symlink accepted")
	}
}
