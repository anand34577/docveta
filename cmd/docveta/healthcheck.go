package main

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/anand34577/docveta/internal/platform/config"
)

// healthcheck asks the running server on this machine for /healthz. It is the Docker
// HEALTHCHECK: the image has no shell or curl. Both the server and the database setup
// page answer, so a fresh install counts as healthy.
func healthcheck() error {
	listen, err := config.ListenAddr()
	if err != nil {
		return err
	}
	u, err := healthURL(listen)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s answered %s", u, resp.Status)
	}
	return nil
}

// healthURL turns a listen address (":8080", "0.0.0.0:8080", "[::]:8080",
// "192.168.1.5:8080") into the address to check it at from the same machine.
func healthURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", fmt.Errorf("DOCVETA_LISTEN %q: %w", listen, err)
	}
	if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz", nil
}
