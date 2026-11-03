package unixsocket

import "golang.org/x/sys/unix"

// maxPath is sun_path (104 bytes) without its terminating NUL.
const maxPath = 103

func peerUID(fd int) (uint32, error) {
	credentials, err := unix.GetsockoptXucred(fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
	if err != nil {
		return 0, err
	}
	return credentials.Uid, nil
}
