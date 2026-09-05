//go:build !linux

package cmd

func bindSocketToDevice(fd uintptr, device string) error {
	return nil
}
