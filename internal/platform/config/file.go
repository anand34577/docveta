package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// FileName is the configuration file Docveta reads. Each line is KEY=VALUE using the
// same names as the environment variables; # starts a comment.
//
// Docveta looks for it in this order, and environment variables always win:
//  1. the path in DOCVETA_CONFIG, or docveta.conf next to the executable (written by the
//     installer or by hand: data folder, port, OCR device, …)
//  2. docveta.conf in the data folder (written by Docveta itself: database settings from
//     the setup page, the generated secret key)
const FileName = "docveta.conf"

// primaryFile returns the admin-maintained config file path (it may not exist).
func primaryFile() string {
	if p := os.Getenv("DOCVETA_CONFIG"); p != "" {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), FileName)
}

// loadFile sets environment variables from a config file, without overriding
// variables that are already set to a non-empty value. A missing file is not an error.
func loadFile(path string) error {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		// Empty variables count as unset (Docker Compose passes KEY= for unset .env entries).
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return sc.Err()
}

func parseLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	k, v, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	k, v = strings.TrimSpace(k), strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' && v[len(v)-1] == '"' || v[0] == '\'' && v[len(v)-1] == '\'') {
		v = v[1 : len(v)-1]
	}
	return k, v, k != ""
}

// DataFile is the config file Docveta writes in the data folder.
func DataFile(dataDir string) string { return filepath.Join(dataDir, FileName) }

// Save sets keys in the data-folder config file, keeping other lines and comments.
// The file holds secrets (database password, secret key), so it is private to the
// account Docveta runs as.
func Save(dataDir string, values map[string]string) error {
	path := DataFile(dataDir)
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return err
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	} else {
		lines = []string{"# Written by Docveta. Keep this file private and include it in backups.", ""}
	}
	done := map[string]bool{}
	for i, l := range lines {
		if k, _, ok := parseLine(l); ok {
			if v, found := values[k]; found {
				lines[i] = k + "=" + v
				done[k] = true
			}
		}
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if !done[k] {
			lines = append(lines, k+"="+values[k])
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ensureSecret returns DOCVETA_SECRET_KEY, generating and saving one on first start.
// withPasswordFromFile puts the (trimmed) contents of file into dbURL as the password.
func withPasswordFromFile(dbURL, file string) (string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(dbURL)
	if err != nil || u.User == nil {
		return "", errors.New("DOCVETA_DATABASE_URL needs a user name, e.g. postgres://docveta@db:5432/docveta")
	}
	u.User = url.UserPassword(u.User.Username(), strings.TrimSpace(string(b)))
	return u.String(), nil
}

// ensureKeyFile returns the key in file, creating it with a random key on first use.
// The file is world-readable on purpose: it lives in a volume shared only with the
// worker containers, which run as other users.
func ensureKeyFile(file string) ([]byte, error) {
	if b, err := os.ReadFile(file); err == nil && len(strings.TrimSpace(string(b))) >= 32 {
		return []byte(strings.TrimSpace(string(b))), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	key := hex.EncodeToString(b)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return nil, err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, []byte(key+"\n"), 0o644); err != nil {
		return nil, err
	}
	return []byte(key), os.Rename(tmp, file)
}

func ensureSecret(dataDir string) (string, error) {
	if s := os.Getenv("DOCVETA_SECRET_KEY"); s != "" {
		return s, nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	if err := Save(dataDir, map[string]string{"DOCVETA_SECRET_KEY": s}); err != nil {
		return "", err
	}
	_ = os.Setenv("DOCVETA_SECRET_KEY", s)
	return s, nil
}
