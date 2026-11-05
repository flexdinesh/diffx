//go:build !linux && !darwin

package app

import "errors"

func checkRuntime() error { return errors.New("Diffx Desktop supports macOS and Linux") }
