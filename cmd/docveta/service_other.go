//go:build !windows

package main

import (
	"context"
	"errors"
)

func isWindowsService() bool { return false }

func runAsService(func(context.Context, []string) error) error { return nil }

// Linux and macOS use systemd/launchd unit files (deploy/); no code is needed there.
func serviceCmd([]string) error {
	return errors.New("`docveta service` is for Windows. On Linux use the systemd unit (deploy/linux/docveta.service), on macOS the launchd plist (deploy/macos/)")
}
