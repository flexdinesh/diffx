package localprotocol

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

// MaxCaptureBytes bounds native capture spooling before the CLI validates it.
// The CLI's contract test keeps this aligned with its producer input limit.
const MaxCaptureBytes int64 = 16 << 20

// CaptureInput makes inherited blocking pipes pollable before a cancellable
// read. Only the duplicate is closed; shared descriptor flags are restored.
func CaptureInput(ctx context.Context, input *os.File) (*os.File, func(), error) {
	info, err := input.Stat()
	if err != nil {
		return nil, nil, err
	}
	if info.Mode()&(os.ModeNamedPipe|os.ModeSocket) == 0 {
		return input, func() {}, nil
	}
	raw, err := input.SyscallConn()
	if err != nil {
		return nil, nil, err
	}
	var fd, flags int
	var duplicateErr error
	err = raw.Control(func(source uintptr) {
		flags, duplicateErr = unix.FcntlInt(source, unix.F_GETFL, 0)
		if duplicateErr == nil {
			fd, duplicateErr = unix.Dup(int(source))
		}
	})
	if err != nil {
		return nil, nil, err
	}
	if duplicateErr != nil {
		return nil, nil, duplicateErr
	}
	unix.CloseOnExec(fd)
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, nil, err
	}
	file := os.NewFile(uintptr(fd), "desktop-capture")
	stop := context.AfterFunc(ctx, func() { _ = file.Close() })
	return file, func() {
		stop()
		_ = file.Close()
		_ = raw.Control(func(source uintptr) { _ = unix.SetNonblock(int(source), flags&unix.O_NONBLOCK != 0) })
	}, nil
}
