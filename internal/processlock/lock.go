// Package processlock provides kernel-owned process locks. Lock files persist;
// removing them would let another process lock a different inode at the same path.
package processlock

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var ErrLocked = errors.New("process lock held")

type Lock struct {
	file *os.File
	once sync.Once
	err  error
}

func Acquire(path string) (*Lock, error)    { return acquire(path, false) }
func TryAcquire(path string) (*Lock, error) { return acquire(path, true) }

// AcquireContext never leaves a kernel-blocked lock attempt after cancellation.
func AcquireContext(ctx context.Context, path string) (*Lock, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lock, err := TryAcquire(path)
		if !errors.Is(err, ErrLocked) {
			if err == nil && ctx.Err() != nil {
				_ = lock.Close()
				return nil, ctx.Err()
			}
			return lock, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func acquire(path string, nonblocking bool) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(file, nonblocking); err != nil {
		_ = file.Close()
		return nil, err
	}
	return &Lock{file: file}, nil
}

func (lock *Lock) Close() error {
	lock.once.Do(func() { lock.err = errors.Join(unlockFile(lock.file), lock.file.Close()) })
	return lock.err
}
