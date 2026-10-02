package config

import (
	"os"
	"strings"
	"testing"
)

func TestSaveAndLoadFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(DataFile(dir), []byte("# keep me\nDOCVETA_LISTEN = :9000\nDOCVETA_LOG_LEVEL='debug'\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(dir, map[string]string{"DOCVETA_LISTEN": ":9100", "DOCVETA_DATABASE_URL": "postgres://u:p@h/db"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(DataFile(dir))
	got := string(b)
	for _, want := range []string{"# keep me", "DOCVETA_LISTEN=:9100", "DOCVETA_LOG_LEVEL='debug'", "DOCVETA_DATABASE_URL=postgres://u:p@h/db"} {
		if !strings.Contains(got, want) {
			t.Errorf("saved file lacks %q:\n%s", want, got)
		}
	}

	t.Setenv("DOCVETA_LISTEN", ":1234") // environment wins over the file
	t.Setenv("DOCVETA_LOG_LEVEL", "")
	os.Unsetenv("DOCVETA_LOG_LEVEL")
	t.Setenv("DOCVETA_DATABASE_URL", "")
	os.Unsetenv("DOCVETA_DATABASE_URL")
	if err := loadFile(DataFile(dir)); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("DOCVETA_LISTEN") != ":1234" || os.Getenv("DOCVETA_LOG_LEVEL") != "debug" || os.Getenv("DOCVETA_DATABASE_URL") != "postgres://u:p@h/db" {
		t.Errorf("env after load: listen=%q level=%q db=%q", os.Getenv("DOCVETA_LISTEN"), os.Getenv("DOCVETA_LOG_LEVEL"), os.Getenv("DOCVETA_DATABASE_URL"))
	}
}

func TestEnsureSecretGeneratesOnce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DOCVETA_SECRET_KEY", "")
	os.Unsetenv("DOCVETA_SECRET_KEY")
	s1, err := ensureSecret(dir)
	if err != nil || len(s1) != 64 {
		t.Fatalf("secret %q, %v", s1, err)
	}
	os.Setenv("DOCVETA_SECRET_KEY", "") // Docker Compose passes empty values for unset .env entries
	if err := loadFile(DataFile(dir)); err != nil {
		t.Fatal(err)
	}
	if s2, _ := ensureSecret(dir); s2 != s1 {
		t.Fatal("secret changed after reload")
	}
}
