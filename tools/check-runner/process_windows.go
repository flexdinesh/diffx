package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Layout of Windows JOBOBJECT_BASIC_ACCOUNTING_INFORMATION.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

func runCommand(ctx context.Context, directory string, env, args []string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(job)
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		return err
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir, cmd.Env = directory, env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Assign the suspended child before it can create descendants outside the job.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED}
	if err := cmd.Start(); err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	status, _, _ := windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(process))
	if status != 0 {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return windows.NTStatus(status)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err = <-finished:
	case <-ctx.Done():
		err = ctx.Err()
		_ = windows.TerminateJobObject(job, 1)
		<-finished
	}
	if stopErr := windows.TerminateJobObject(job, 1); stopErr != nil {
		return errors.Join(err, stopErr)
	}
	// Termination is asynchronous. Keep ownership until every job member exits.
	for {
		var accounting jobAccounting
		if queryErr := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation, uintptr(unsafe.Pointer(&accounting)), uint32(unsafe.Sizeof(accounting)), nil); queryErr != nil {
			return errors.Join(err, queryErr)
		}
		if accounting.ActiveProcesses == 0 {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
