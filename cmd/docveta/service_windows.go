//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

const serviceName = "Docveta"

func isWindowsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

// runAsService runs fn under the Windows service manager until the service is stopped.
func runAsService(fn func(context.Context, []string) error) error {
	return svc.Run(serviceName, &handler{fn: fn})
}

type handler struct {
	fn func(context.Context, []string) error
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.fn(ctx, []string{"serve"}) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			if err != nil {
				return true, 1 // non-zero exit: recovery actions restart the service
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 30000}
				cancel()
				select {
				case <-done:
				case <-time.After(40 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// serviceCmd implements `docveta service install|uninstall|start|stop`.
func serviceCmd(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: docveta service install|uninstall|start|stop")
	}
	m, err := mgr.Connect()
	if err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return errors.New("managing services needs administrator rights: run this from an administrator command prompt")
		}
		return err
	}
	defer m.Disconnect()

	switch args[0] {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		if s, err := m.OpenService(serviceName); err == nil { // upgrade: keep it, point it at this program
			defer s.Close()
			c, err := s.Config()
			if err != nil {
				return err
			}
			c.BinaryPathName = `"` + exe + `" serve`
			if err := s.UpdateConfig(c); err != nil {
				return err
			}
			fmt.Println("Service \"Docveta\" is already installed; updated it to use", exe)
			return nil
		}
		s, err := m.CreateService(serviceName, exe, mgr.Config{
			DisplayName:      "Docveta Document Manager",
			Description:      "Docveta: organises and searches your documents. Open http://localhost:8080 (or the address in docveta.conf).",
			StartType:        mgr.StartAutomatic,
			DelayedAutoStart: true, // after networking and the database service
		}, "serve")
		if err != nil {
			return fmt.Errorf("create service: %w", err)
		}
		defer s.Close()
		_ = s.SetRecoveryActions([]mgr.RecoveryAction{
			{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
			{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
			{Type: mgr.ServiceRestart, Delay: time.Minute},
		}, 24*60*60)
		fmt.Println("Service \"Docveta\" installed. Start it with: docveta service start")
	case "uninstall":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return fmt.Errorf("the Docveta service isn't installed: %w", err)
		}
		defer s.Close()
		_, _ = s.Control(svc.Stop)
		waitStopped(s) // so the uninstaller doesn't remove files while Docveta shuts down its database
		if err := s.Delete(); err != nil {
			return err
		}
		fmt.Println("Service \"Docveta\" removed. Your data folder was not touched.")
	case "start":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return fmt.Errorf("the Docveta service isn't installed (run: docveta service install): %w", err)
		}
		defer s.Close()
		if err := s.Start(); err != nil {
			return err
		}
		fmt.Println("Service \"Docveta\" started.")
	case "stop":
		s, err := m.OpenService(serviceName)
		if err != nil {
			return err
		}
		defer s.Close()
		if _, err := s.Control(svc.Stop); err != nil {
			return err
		}
		waitStopped(s)
		fmt.Println("Service \"Docveta\" stopped.")
	default:
		return errors.New("usage: docveta service install|uninstall|start|stop")
	}
	return nil
}

// waitStopped waits up to a minute for the service to stop.
func waitStopped(s *mgr.Service) {
	for i := 0; i < 60; i++ {
		if st, err := s.Query(); err != nil || st.State == svc.Stopped {
			return
		}
		time.Sleep(time.Second)
	}
}
