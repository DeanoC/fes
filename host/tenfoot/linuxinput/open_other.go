//go:build !linux

package linuxinput

import "os"

// Discover is a no-op off Linux; the spike opens nothing for "auto".
func Discover() ([]string, error) {
	return nil, nil
}

func deviceName(f *os.File) (string, error) {
	return "", nil
}
