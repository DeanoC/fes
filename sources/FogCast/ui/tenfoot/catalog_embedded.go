//go:build wasip1 || tamago

package tenfoot

import "errors"

// An embedded caller supplies a host-client transport through NewApp. The
// native kit's SQLite service and Unix control socket are not linked here.
func configureLocalCatalog(*App, Options) error {
	return errors.New("menu-display local catalog requires the native host service; use RunPlatform with an explicit client transport")
}
