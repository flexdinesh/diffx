package unixsocket

import "golang.org/x/sys/unix"

// maxPath is sun_path (108 bytes) without its terminating NUL.
const maxPath = 107

func peerUID(fd int) (uint32, error) {
	credentials, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return 0, err
	}
	return credentials.Uid, nil
}
