//go:build !windows

package app

import "os/exec"

// prepareChild asks Linux to stop the child if Docveta dies (see child_linux.go);
// elsewhere a stale engine exits once its worker token is renewed at the next start.
func prepareChild(cmd *exec.Cmd) { setPdeathsig(cmd) }

func killWithParent(*exec.Cmd) func() { return func() {} }
