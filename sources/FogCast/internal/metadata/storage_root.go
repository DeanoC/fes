package metadata

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type storageRootHooks struct {
	beforeOpen   func(path string)
	beforeCreate func(path string)
	beforeChmod  func(path string)
}

var storageRootHookState struct {
	sync.Mutex
	hooks storageRootHooks
}

func setStorageRootHooksForTest(hooks storageRootHooks) func() {
	storageRootHookState.Lock()
	previous := storageRootHookState.hooks
	storageRootHookState.hooks = hooks
	storageRootHookState.Unlock()
	return func() {
		storageRootHookState.Lock()
		storageRootHookState.hooks = previous
		storageRootHookState.Unlock()
	}
}

func storageRootHooksSnapshot() storageRootHooks {
	storageRootHookState.Lock()
	defer storageRootHookState.Unlock()
	return storageRootHookState.hooks
}

type componentGuard struct {
	parent     *os.Root
	parentInfo os.FileInfo
	name       string
	path       string
	childInfo  os.FileInfo
}

type createdDir struct {
	parent *os.Root
	name   string
	info   os.FileInfo
}

// privateRoot is the descriptor-owned authority for one configured metadata
// directory. Its path is only an initialization identity used while guards are
// live; operations after commit use root exclusively.
type privateRoot struct {
	path           string
	configuredPath string
	root           *os.Root
	info           os.FileInfo
	guards         []componentGuard
	created        []createdDir
}

func acquirePrivateRoot(path string, create bool) (*privateRoot, error) {
	volumeRoot, parts, err := splitPrivateRootPath(path)
	if err != nil {
		return nil, err
	}
	current, err := os.OpenRoot(volumeRoot)
	if err != nil {
		return nil, err
	}
	owner := &privateRoot{path: volumeRoot, configuredPath: path}
	owner.root = current
	cleanup := func(cause error) (*privateRoot, error) {
		cleanupErr := owner.abort(true)
		return nil, errors.Join(cause, cleanupErr)
	}

	logical := volumeRoot
	for index, part := range parts {
		info, statErr := current.Lstat(part)
		created := false
		openName := part
		childPath := filepath.Join(logical, part)
		if errors.Is(statErr, fs.ErrNotExist) {
			if !create {
				return cleanup(fs.ErrNotExist)
			}
			hooks := storageRootHooksSnapshot()
			if hooks.beforeCreate != nil {
				hooks.beforeCreate(storageLexicalComponentPath(volumeRoot, parts, index))
			}
			mkdirErr := current.Mkdir(part, 0o700)
			if mkdirErr != nil && !errors.Is(mkdirErr, fs.ErrExist) {
				return cleanup(mkdirErr)
			}
			created = mkdirErr == nil
			info, statErr = current.Lstat(part)
			if statErr != nil {
				return cleanup(statErr)
			}
		} else if statErr != nil {
			return cleanup(statErr)
		}
		if created {
			rollbackParent, rollbackErr := current.OpenRoot(".")
			if rollbackErr != nil {
				return cleanup(rollbackErr)
			}
			owner.created = append(owner.created, createdDir{parent: rollbackParent, name: part, info: info})
		}

		if info.Mode()&os.ModeSymlink != 0 {
			if index == len(parts)-1 || !isAllowedSystemAlias(childPath) {
				return cleanup(errors.New("metadata root has an unsafe parent"))
			}
			target, readErr := current.Readlink(part)
			if readErr != nil || !allowedSystemAliasTarget(childPath, target) {
				return cleanup(errors.New("metadata root has an unsafe parent"))
			}
			openName = filepath.Clean(target)
			childPath = filepath.Join(volumeRoot, openName)
			info, statErr = current.Lstat(openName)
			if statErr != nil {
				return cleanup(statErr)
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return cleanup(errors.New("metadata root alias target is unsafe"))
			}
		} else if !info.IsDir() {
			return cleanup(errors.New("metadata root component is not a directory"))
		}

		hooks := storageRootHooksSnapshot()
		if hooks.beforeOpen != nil {
			hooks.beforeOpen(storageLexicalComponentPath(volumeRoot, parts, index))
		}
		next, openErr := current.OpenRoot(openName)
		if openErr != nil {
			return cleanup(openErr)
		}
		openedInfo, statErr := next.Stat(".")
		if statErr != nil || openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.IsDir() {
			_ = next.Close()
			if statErr == nil {
				statErr = errors.New("metadata root component is unsafe")
			}
			return cleanup(statErr)
		}
		if openedInfo.Mode()&os.ModeSymlink != 0 || !openedInfo.IsDir() {
			_ = next.Close()
			return cleanup(errors.New("metadata root component is unsafe"))
		}
		if sameErr := verifyOpenedComponent(current, openName, openedInfo); sameErr != nil {
			_ = next.Close()
			return cleanup(sameErr)
		}

		shouldChmod := created || index == len(parts)-1
		if shouldChmod {
			if hooks.beforeChmod != nil {
				hooks.beforeChmod(storageLexicalComponentPath(volumeRoot, parts, index))
			}
			if chmodErr := chmodRootDirectory(next); chmodErr != nil {
				_ = next.Close()
				return cleanup(chmodErr)
			}
			if sameErr := verifyOpenedComponent(current, openName, openedInfo); sameErr != nil {
				_ = next.Close()
				return cleanup(sameErr)
			}
		}

		parentClone, cloneErr := current.OpenRoot(".")
		if cloneErr != nil {
			_ = next.Close()
			return cleanup(cloneErr)
		}
		parentInfo, parentStatErr := current.Stat(".")
		if parentStatErr != nil {
			_ = parentClone.Close()
			_ = next.Close()
			return cleanup(parentStatErr)
		}
		owner.guards = append(owner.guards, componentGuard{parent: parentClone, parentInfo: parentInfo, name: openName, path: childPath, childInfo: openedInfo})

		if err := owner.verifyGuards(); err != nil {
			_ = next.Close()
			return cleanup(err)
		}
		_ = current.Close()
		current = next
		owner.root = current
		logical = childPath
		owner.path = childPath
	}

	owner.root = current
	owner.info, err = current.Stat(".")
	if err != nil {
		return cleanup(err)
	}
	if err := owner.verifyInitChain(); err != nil {
		return cleanup(err)
	}
	return owner, nil
}

func storageLexicalComponentPath(volumeRoot string, parts []string, index int) string {
	path := volumeRoot
	for _, part := range parts[:index+1] {
		path = filepath.Join(path, part)
	}
	return path
}

func splitPrivateRootPath(path string) (string, []string, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", nil, errors.New("metadata root is unsafe")
	}
	volumeRoot := filepath.VolumeName(path) + string(filepath.Separator)
	if volumeRoot == string(filepath.Separator) && filepath.VolumeName(path) == "" {
		volumeRoot = string(filepath.Separator)
	}
	if filepath.Clean(path) == filepath.Clean(volumeRoot) {
		return "", nil, errors.New("metadata root is unsafe")
	}
	relative := strings.TrimPrefix(path, volumeRoot)
	if relative == path || relative == "" {
		return "", nil, errors.New("metadata root is unsafe")
	}
	parts := make([]string, 0, 8)
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		if part == "" || part == "." || part == ".." {
			return "", nil, errors.New("metadata root is unsafe")
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "", nil, errors.New("metadata root is unsafe")
	}
	return volumeRoot, parts, nil
}

func verifyOpenedComponent(parent *os.Root, name string, opened os.FileInfo) error {
	parentInfo, err := parent.Lstat(name)
	if err != nil {
		return err
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() || !os.SameFile(parentInfo, opened) {
		return errors.New("metadata root component identity changed")
	}
	return nil
}

func chmodRootDirectory(root *os.Root) error {
	file, err := root.Open(".")
	if err != nil {
		return err
	}
	chmodErr := file.Chmod(0o700)
	closeErr := file.Close()
	return errors.Join(chmodErr, closeErr)
}

func (r *privateRoot) clone() (*os.Root, os.FileInfo, error) {
	if r == nil || r.root == nil {
		return nil, nil, errors.New("metadata root is unavailable")
	}
	clone, err := r.root.OpenRoot(".")
	if err != nil {
		return nil, nil, err
	}
	info, err := clone.Stat(".")
	if err != nil || !info.IsDir() || !os.SameFile(r.info, info) {
		_ = clone.Close()
		if err == nil {
			err = errors.New("metadata root clone identity changed")
		}
		return nil, nil, err
	}
	return clone, info, nil
}

func (r *privateRoot) verifyGuards() error {
	if r == nil {
		return errors.New("metadata root is unavailable")
	}
	for _, guard := range r.guards {
		if guard.parent == nil {
			return errors.New("metadata root guard is unavailable")
		}
		parentInfo, err := guard.parent.Stat(".")
		if err != nil || !os.SameFile(parentInfo, guard.parentInfo) {
			if err == nil {
				err = errors.New("metadata root parent identity changed")
			}
			return err
		}
		held, err := guard.parent.Lstat(guard.name)
		if err != nil {
			return err
		}
		if held.Mode()&os.ModeSymlink != 0 || !held.IsDir() || !os.SameFile(held, guard.childInfo) {
			return errors.New("metadata root component identity changed")
		}
		ambient, err := os.Lstat(guard.path)
		if err != nil {
			return err
		}
		if ambient.Mode()&os.ModeSymlink != 0 || !ambient.IsDir() || !os.SameFile(ambient, guard.childInfo) {
			return errors.New("metadata root ambient identity changed")
		}
	}
	return nil
}

func (r *privateRoot) verifyInitChain() error {
	if r == nil || r.root == nil {
		return errors.New("metadata root is unavailable")
	}
	if err := r.verifyGuards(); err != nil {
		return err
	}
	held, err := r.root.Stat(".")
	if err != nil {
		return err
	}
	ambient, err := os.Lstat(r.path)
	if err != nil {
		return err
	}
	if held.Mode()&os.ModeSymlink != 0 || !held.IsDir() || ambient.Mode()&os.ModeSymlink != 0 || !ambient.IsDir() || !os.SameFile(held, ambient) || !os.SameFile(r.info, held) {
		return errors.New("metadata root identity changed during initialization")
	}
	return nil
}

func (r *privateRoot) commit() error {
	if err := r.verifyInitChain(); err != nil {
		return err
	}
	for index := range r.created {
		if r.created[index].parent != nil {
			_ = r.created[index].parent.Close()
			r.created[index].parent = nil
		}
	}
	r.created = nil
	for index := range r.guards {
		if r.guards[index].parent != nil {
			_ = r.guards[index].parent.Close()
			r.guards[index].parent = nil
		}
	}
	r.guards = nil
	return nil
}

func (r *privateRoot) abort(removeCreated bool) error {
	if r == nil {
		return nil
	}
	var errs []error
	if removeCreated {
		for index := len(r.created) - 1; index >= 0; index-- {
			created := r.created[index]
			if created.parent == nil {
				continue
			}
			info, err := created.parent.Lstat(created.name)
			if errors.Is(err, fs.ErrNotExist) {
				if closeErr := created.parent.Close(); closeErr != nil {
					errs = append(errs, closeErr)
				}
				r.created[index].parent = nil
				continue
			}
			if err != nil {
				errs = append(errs, err)
				if closeErr := created.parent.Close(); closeErr != nil {
					errs = append(errs, closeErr)
				}
				r.created[index].parent = nil
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(info, created.info) {
				errs = append(errs, errors.New("metadata root rollback identity changed"))
				if closeErr := created.parent.Close(); closeErr != nil {
					errs = append(errs, closeErr)
				}
				r.created[index].parent = nil
				continue
			}
			if err := created.parent.Remove(created.name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				errs = append(errs, err)
			}
			if closeErr := created.parent.Close(); closeErr != nil {
				errs = append(errs, closeErr)
			}
			r.created[index].parent = nil
		}
	}
	if r.root != nil {
		if err := r.root.Close(); err != nil {
			errs = append(errs, err)
		}
		r.root = nil
	}
	for index := range r.guards {
		if r.guards[index].parent != nil {
			if err := r.guards[index].parent.Close(); err != nil {
				errs = append(errs, err)
			}
			r.guards[index].parent = nil
		}
	}
	r.guards = nil
	for index := range r.created {
		if r.created[index].parent != nil {
			if err := r.created[index].parent.Close(); err != nil {
				errs = append(errs, err)
			}
			r.created[index].parent = nil
		}
	}
	r.created = nil
	return errors.Join(errs...)
}

func allowedSystemAliasTarget(path, target string) bool {
	cleanTarget := filepath.Clean(target)
	switch filepath.Clean(path) {
	case filepath.Clean("/tmp"):
		return cleanTarget == filepath.Clean("private/tmp")
	case filepath.Clean("/var"):
		return cleanTarget == filepath.Clean("private/var")
	default:
		return false
	}
}

func isAllowedSystemAlias(path string) bool {
	clean := filepath.Clean(path)
	return clean == filepath.Clean("/var") || clean == filepath.Clean("/tmp")
}
