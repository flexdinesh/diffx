package localprotocol

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const HandoffEnvironment = "DIFFX_DESKTOP_HANDOFF"

// Intent is native-only. Never expose it through a webview bridge or diagnostic.
type Intent struct {
	Version     int      `json:"version"`
	Args        []string `json:"args"`
	Directory   string   `json:"directory"`
	Sidecar     string   `json:"sidecar"`
	Stdin       bool     `json:"stdin"`
	Environment []string `json:"environment"`
}

type Acceptance struct {
	Accepted bool   `json:"accepted"`
	Error    string `json:"error,omitempty"`
}

// RuntimeDirectory is deliberately short enough for Darwin's Unix socket limit.
// Runtime overrides isolate development and acceptance from the user's app.
func RuntimeDirectory() (string, error) {
	suffix := ""
	if runtime := os.Getenv("DIFFX_RUNTIME_DIR"); runtime != "" {
		suffix = fmt.Sprintf("-%x", sha256.Sum256([]byte(runtime)))[:13]
	}
	dir := filepath.Join("/tmp", fmt.Sprintf("diffx-desktop-%d%s", os.Getuid(), suffix))
	if err := os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	var stat unix.Stat_t
	if err := unix.Lstat(dir, &stat); err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return "", errors.New("desktop runtime directory is not owner-only")
	}
	return dir, nil
}

var ErrRunning = errors.New("desktop already running")

type Inbox struct {
	listener    *net.UnixListener
	lock        *os.File
	mu          sync.Mutex
	closed      bool
	connections map[*net.UnixConn]struct{}
	wg          sync.WaitGroup
}

// Listen holds the singleton lock before removing a stale socket. Permissions
// and peer credentials both restrict launch intents to this OS user.
func Listen(handle func(context.Context, Intent, *os.File) Acceptance) (*Inbox, error) {
	dir, err := RuntimeDirectory()
	if err != nil {
		return nil, err
	}
	fd, err := unix.Open(filepath.Join(dir, "lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	lock := os.NewFile(uintptr(fd), "desktop-lock")
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, ErrRunning
		}
		return nil, err
	}
	path := filepath.Join(dir, "launch.sock")
	_ = os.Remove(path)
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err == nil {
		err = os.Chmod(path, 0600)
	}
	if err != nil {
		if listener != nil {
			listener.Close()
		}
		lock.Close()
		return nil, err
	}
	i := &Inbox{listener: listener, lock: lock, connections: make(map[*net.UnixConn]struct{})}
	i.wg.Add(1)
	go func() {
		defer i.wg.Done()
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			i.mu.Lock()
			if i.closed {
				i.mu.Unlock()
				conn.Close()
				return
			}
			i.connections[conn] = struct{}{}
			i.wg.Add(1)
			i.mu.Unlock()
			go func() {
				defer i.wg.Done()
				defer func() { conn.Close(); i.mu.Lock(); delete(i.connections, conn); i.mu.Unlock() }()
				if err := ownerPeer(conn); err != nil {
					return
				}
				_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				intent, input, err := receive(conn)
				if input != nil {
					defer input.Close()
				}
				if err != nil {
					_ = json.NewEncoder(conn).Encode(Acceptance{Error: "invalid desktop launch request"})
					return
				}
				_ = conn.SetReadDeadline(time.Time{})
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				go func() { var b [1]byte; _, _ = conn.Read(b[:]); cancel() }()
				_ = json.NewEncoder(conn).Encode(handle(ctx, intent, input))
			}()
		}
	}()
	return i, nil
}

func (i *Inbox) Close() {
	i.mu.Lock()
	i.closed = true
	_ = i.listener.Close()
	for conn := range i.connections {
		_ = conn.Close()
	}
	i.mu.Unlock()
	i.wg.Wait()
	_ = i.lock.Close()
}

func Send(ctx context.Context, intent Intent, input *os.File) (Acceptance, error) {
	dir, err := RuntimeDirectory()
	if err != nil {
		return Acceptance{}, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", filepath.Join(dir, "launch.sock"))
	if err != nil {
		return Acceptance{}, err
	}
	defer conn.Close()
	u, ok := conn.(*net.UnixConn)
	if !ok {
		return Acceptance{}, errors.New("invalid desktop IPC")
	}
	if err := ownerPeer(u); err != nil {
		return Acceptance{}, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	rights := []byte(nil)
	if input != nil {
		rights = unix.UnixRights(int(input.Fd()))
	}
	intent.Stdin = input != nil
	if _, _, err := u.WriteMsgUnix([]byte{1}, rights, nil); err != nil {
		return Acceptance{}, err
	}
	if err := json.NewEncoder(conn).Encode(intent); err != nil {
		return Acceptance{}, err
	}
	var reply Acceptance
	err = json.NewDecoder(io.LimitReader(conn, 4096)).Decode(&reply)
	return reply, err
}

func receive(conn *net.UnixConn) (Intent, *os.File, error) {
	var marker [1]byte
	oob := make([]byte, unix.CmsgSpace(4))
	n, on, flags, _, err := conn.ReadMsgUnix(marker[:], oob)
	if err != nil {
		return Intent{}, nil, err
	}
	var input *os.File
	if on > 0 {
		messages, err := unix.ParseSocketControlMessage(oob[:on])
		if err != nil {
			return Intent{}, nil, err
		}
		for _, message := range messages {
			fds, err := unix.ParseUnixRights(&message)
			if err != nil {
				if input != nil {
					input.Close()
				}
				return Intent{}, nil, err
			}
			for _, fd := range fds {
				unix.CloseOnExec(fd)
				if input == nil {
					input = os.NewFile(uintptr(fd), "desktop-stdin")
				} else {
					unix.Close(fd)
				}
			}
		}
	}
	var intent Intent
	if n != 1 || marker[0] != 1 || flags&unix.MSG_CTRUNC != 0 {
		return intent, input, errors.New("invalid IPC descriptors")
	}
	err = json.NewDecoder(io.LimitReader(conn, 64*1024)).Decode(&intent)
	if err == nil && (intent.Version != Version || !filepath.IsAbs(intent.Directory) || !filepath.IsAbs(intent.Sidecar) || intent.Stdin != (input != nil)) {
		err = errors.New("invalid desktop intent")
	}
	return intent, input, err
}

func ownerPeer(conn *net.UnixConn) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	var peerErr error
	err = raw.Control(func(fd uintptr) {
		var uid uint32
		uid, peerErr = peerUID(int(fd))
		if peerErr == nil && uid != uint32(os.Getuid()) {
			peerErr = errors.New("desktop IPC owner denied")
		}
	})
	return errors.Join(err, peerErr)
}
