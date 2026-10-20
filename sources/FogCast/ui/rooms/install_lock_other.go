//go:build !unix

package rooms

import "errors"

func lockInstall(string) (func(), error) {
	return nil, errors.New("room installation requires POSIX file locking")
}
