//go:build linux

package cmd

import (
	"golang.org/x/sys/unix"
)

func bindSocketToDevice(fd uintptr, device string) error {
	if device == "" {
		return nil
	}
	return unix.BindToDevice(int(fd), device)
}
