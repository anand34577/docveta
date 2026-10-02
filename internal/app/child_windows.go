//go:build windows

package app

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func prepareChild(*exec.Cmd) {}

// killWithParent makes Windows end the started child when Docveta exits for any reason
// (also a crash or a forced stop), using a job object with "kill on close". Call the
// returned function after the child has exited.
func killWithParent(cmd *exec.Cmd) func() {
	none := func() {}
	if cmd.Process == nil {
		return none
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return none
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		defer windows.CloseHandle(h)
		_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	}
	if err == nil {
		err = windows.AssignProcessToJobObject(job, h)
	}
	if err != nil {
		windows.CloseHandle(job)
		return none
	}
	return func() { windows.CloseHandle(job) }
}
