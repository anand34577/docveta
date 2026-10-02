package app

import (
	"bufio"
	"context"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anand34577/docveta/internal/auth"
	"github.com/anand34577/docveta/internal/platform/db"
)

const localOCRName = "local-ocr"

// findLocalOCR returns the bundled OCR engine shipped next to the docveta executable
// (ocr/docveta-ocr or docveta-ocr), or the path given in DOCVETA_LOCAL_OCR.
func findLocalOCR(setting string) string {
	switch setting {
	case "off", "false", "0":
		return ""
	case "", "auto":
	default:
		return setting // explicit path
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	name := "docveta-ocr"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	dir := filepath.Dir(exe)
	for _, p := range []string{filepath.Join(dir, "ocr", name), filepath.Join(dir, name)} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// runLocalOCR starts the bundled OCR engine as a worker and keeps it running until
// ctx ends, so a fresh install recognises text without any setup. The engine picks
// GPU, NPU or CPU itself (DOCVETA_OCR_DEVICE, see docs). It registers as the worker
// "local-ocr" with a token that is renewed on every start.
func (a *App) runLocalOCR(ctx context.Context) {
	exe := findLocalOCR(a.Cfg.LocalOCR)
	if exe == "" {
		return
	}
	log := a.Log.With("component", "local-ocr")
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		token, err := a.localOCRToken(ctx)
		if err != nil {
			log.Error("can't register the local OCR engine", "err", err)
			return
		}
		cmd := exec.CommandContext(ctx, exe)
		cmd.Env = append(childEnv(), "DOCVETA_URL="+localURL(a.Cfg.ListenAddr), "DOCVETA_WORKER_TOKEN="+token, "DOCVETA_WORKER_NAME="+localOCRName)
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		prepareChild(cmd)
		started := time.Now()
		log.Info("starting the bundled OCR engine", "path", exe)
		if err := cmd.Start(); err != nil {
			log.Error("can't start the bundled OCR engine", "path", exe, "err", err)
			return
		}
		release := killWithParent(cmd)
		go pipeLog(stdout, log)
		err = cmd.Wait()
		release()
		if ctx.Err() != nil {
			return
		}
		if time.Since(started) > 5*time.Minute {
			backoff = 5 * time.Second // it ran fine for a while; restart quickly
		}
		log.Warn("the bundled OCR engine stopped; restarting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 5*time.Minute)
	}
}

// localOCRToken creates the local-ocr worker on first use, or issues it a new token.
func (a *App) localOCRToken(ctx context.Context) (string, error) {
	sys := auth.System()
	var id uuid.UUID
	err := a.Pool.QueryRow(ctx, `SELECT id FROM workers WHERE name=$1`, localOCRName).Scan(&id)
	if db.IsNoRows(err) {
		_, token, err := a.Pipeline.CreateWorker(ctx, sys, localOCRName)
		return token, err
	}
	if err != nil {
		return "", err
	}
	return a.Pipeline.RotateWorkerToken(ctx, sys, id)
}

// childEnv is the environment minus Docveta's own secrets, which the engine doesn't need.
func childEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		switch strings.ToUpper(k) {
		case "DOCVETA_DATABASE_URL", "DOCVETA_SECRET_KEY", "DOCVETA_PASSWORD", "DOCVETA_WORKER_TOKEN":
			continue
		}
		env = append(env, kv)
	}
	return env
}

// localURL turns the listen address into a URL the engine can reach on this machine.
func localURL(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "http://127.0.0.1:8080"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func pipeLog(r io.Reader, log interface{ Info(string, ...any) }) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		log.Info(sc.Text())
	}
}
