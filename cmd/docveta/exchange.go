package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/anand34577/docveta/internal/app"
	"github.com/anand34577/docveta/internal/exchange"
	"github.com/anand34577/docveta/internal/platform/config"
)

func exportCmd(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	dir := fs.String("dir", "", "write the export into this folder")
	zipFile := fs.String("zip", "", "write the export as this zip file")
	hashes := fs.Bool("with-password-hashes", false, "include password hashes (to move a whole installation)")
	_ = fs.Parse(args)
	if (*dir == "") == (*zipFile == "") {
		return errors.New("give exactly one of --dir and --zip")
	}
	a, err := app.New(ctx, cfg, log, version, false)
	if err != nil {
		return err
	}
	defer a.Close()
	var sink exchange.Sink
	if *dir != "" {
		if err := os.MkdirAll(*dir, 0o750); err != nil {
			return err
		}
		sink = exchange.DirSink(*dir)
	} else {
		f, err := os.Create(*zipFile)
		if err != nil {
			return err
		}
		defer f.Close()
		sink = exchange.ZipSink(f)
	}
	counts, err := exchange.Export(ctx, a.Pool, a.Store, sink, exchange.ExportOptions{Version: version, PasswordHashes: *hashes})
	if err != nil {
		return err
	}
	fmt.Printf("exported %d documents, %d notes, %d users, %d spaces\n", counts["documents"], counts["notes"], counts["users"], counts["spaces"])
	if !*hashes {
		fmt.Println("passwords are not included: people sign in again after an import (an administrator resets them, or they use single sign-on)")
	}
	return nil
}

func importCmd(ctx context.Context, cfg *config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	dir := fs.String("dir", "", "the export folder to read")
	_ = fs.Parse(args)
	if *dir == "" {
		return errors.New("--dir is required (unzip a zip export first)")
	}
	a, err := app.New(ctx, cfg, log, version, false)
	if err != nil {
		return err
	}
	defer a.Close()
	rep, err := exchange.Import(ctx, a.Pool, a.Store, *dir)
	if err != nil {
		return err
	}
	fmt.Printf("imported %d documents (%d already there), %d notes, %d new users, %d new spaces\n", rep.Documents, rep.Skipped, rep.Notes, rep.Users, rep.Spaces)
	for _, u := range rep.UnsetUsers {
		fmt.Printf("  %s has no password: reset it with `docveta user reset-password --email %s`\n", u, u)
	}
	for _, w := range rep.Warnings {
		fmt.Println("  warning:", w)
	}
	return nil
}
