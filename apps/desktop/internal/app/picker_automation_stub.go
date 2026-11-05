//go:build !desktopautomation || mygo_noinspector || !linux || (!amd64 && !arm64)

package app

func bindDesktopTest(*Desktop) {}
