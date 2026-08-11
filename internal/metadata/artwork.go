package metadata

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

const artworkTransformCover = "t_cover_big_2x"
const artworkTransformBackdrop = "t_screenshot_big_2x"

var imageIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var digestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ArtworkFetcherConfig struct {
	Root       string
	HTTPClient *http.Client
}

type ArtworkObject struct {
	Handle  string
	Digest  string
	MIME    string
	Size    int64
	Width   int
	Height  int
	Created int64
}

type ArtworkFetcher struct {
	root         string
	artwork      string
	metadataRoot *os.Root
	artworkRoot  *artworkDirectory
	client       *http.Client
	writeMu      sync.Mutex
	closedMu     sync.RWMutex
	closed       bool
}

// artworkDirectory owns a descriptor-relative handle for the complete
// derived-artifact tree. The path is retained only for identity checks; all
// artifact operations use root-relative names so a swapped parent cannot
// redirect them outside the opened directory.
type artworkDirectory struct {
	path       string
	parent     *os.Root
	parentInfo os.FileInfo
	info       os.FileInfo
	root       *os.Root
	created    bool
}

func openArtworkDirectory(owner *privateRoot, create bool) (*artworkDirectory, error) {
	if owner == nil || owner.root == nil {
		return nil, errors.New("metadata root is unavailable")
	}
	parent, parentInfo, err := owner.clone()
	if err != nil {
		return nil, err
	}
	created := false
	var createdInfo os.FileInfo
	cleanup := func(cause error) (*artworkDirectory, error) {
		if created {
			if info, statErr := parent.Lstat("artwork"); statErr == nil && info.Mode().IsDir() && os.SameFile(info, createdInfo) {
				_ = parent.Remove("artwork")
			}
		}
		_ = parent.Close()
		return nil, cause
	}
	info, err := parent.Lstat("artwork")
	if errors.Is(err, os.ErrNotExist) {
		if !create {
			return cleanup(err)
		}
		hooks := storageRootHooksSnapshot()
		hookPath := filepath.Join(owner.configuredPath, "artwork")
		if hooks.beforeCreate != nil {
			hooks.beforeCreate(hookPath)
		}
		mkdirErr := parent.Mkdir("artwork", 0o700)
		if mkdirErr != nil && !errors.Is(mkdirErr, os.ErrExist) {
			return cleanup(mkdirErr)
		}
		created = mkdirErr == nil
		info, err = parent.Lstat("artwork")
		if err != nil {
			return cleanup(err)
		}
		if created {
			createdInfo = info
		}
	} else if err != nil {
		return cleanup(err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return cleanup(errors.New("artwork root is unsafe"))
	}
	child, err := parent.OpenRoot("artwork")
	if err != nil {
		return cleanup(err)
	}
	cleanupChild := func(cause error) (*artworkDirectory, error) {
		_ = child.Close()
		return cleanup(cause)
	}
	bound, err := child.Stat(".")
	if err != nil || !bound.IsDir() || bound.Mode()&os.ModeSymlink != 0 || !os.SameFile(info, bound) {
		if err == nil {
			err = errors.New("artwork root identity changed during open")
		}
		return cleanupChild(err)
	}
	if err := verifyArtworkChild(parent, info, bound); err != nil {
		return cleanupChild(err)
	}
	hooks := storageRootHooksSnapshot()
	path := filepath.Join(owner.path, "artwork")
	if hooks.beforeChmod != nil {
		hooks.beforeChmod(filepath.Join(owner.configuredPath, "artwork"))
	}
	if err := chmodRootDirectory(child); err != nil {
		return cleanupChild(err)
	}
	if err := verifyArtworkChild(parent, info, bound); err != nil {
		return cleanupChild(err)
	}
	directory := &artworkDirectory{path: path, parent: parent, parentInfo: parentInfo, info: bound, root: child, created: created}
	if err := directory.intact(); err != nil {
		return cleanupChild(err)
	}
	return directory, nil
}

func (d *artworkDirectory) intact() error {
	if d == nil || d.root == nil || d.parent == nil {
		return errors.New("artwork root is unavailable")
	}
	parentInfo, err := d.parent.Stat(".")
	if err != nil || !os.SameFile(d.parentInfo, parentInfo) {
		if err == nil {
			err = errors.New("artwork parent identity changed")
		}
		return err
	}
	bound, err := d.root.Stat(".")
	if err != nil {
		return err
	}
	current, err := d.parent.Lstat("artwork")
	if err != nil {
		return err
	}
	if !bound.IsDir() || bound.Mode()&os.ModeSymlink != 0 || current.Mode()&os.ModeSymlink != 0 || !current.IsDir() ||
		!os.SameFile(d.info, bound) || !os.SameFile(d.info, current) {
		return errors.New("artwork root identity changed")
	}
	return nil
}

func verifyArtworkChild(parent *os.Root, current, opened os.FileInfo) error {
	if current.Mode()&os.ModeSymlink != 0 || !current.IsDir() || opened.Mode()&os.ModeSymlink != 0 || !opened.IsDir() || !os.SameFile(current, opened) {
		return errors.New("artwork root identity changed")
	}
	if parentInfo, err := parent.Stat("."); err != nil {
		return err
	} else if !parentInfo.IsDir() {
		return errors.New("artwork parent is not a directory")
	}
	return nil
}

func (d *artworkDirectory) beforeUse() error {
	if err := d.intact(); err != nil {
		return err
	}
	if hook := derivedArtifactBeforeUseHook(); hook != nil {
		hook()
	}
	return d.intact()
}

func (d *artworkDirectory) readDir() ([]os.DirEntry, error) {
	if err := d.intact(); err != nil {
		return nil, err
	}
	directory, err := d.root.Open(".")
	if err != nil {
		return nil, err
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})
	return entries, nil
}

func (d *artworkDirectory) lstat(name string) (os.FileInfo, error) {
	return d.root.Lstat(name)
}

func (d *artworkDirectory) open(name string) (*os.File, error) {
	return d.root.Open(name)
}

func (d *artworkDirectory) openFile(name string, flag int, mode os.FileMode) (*os.File, error) {
	return d.root.OpenFile(name, flag, mode)
}

func (d *artworkDirectory) remove(name string) error {
	return d.root.Remove(name)
}

func (d *artworkDirectory) rename(oldName, newName string) error {
	return d.root.Rename(oldName, newName)
}

func (d *artworkDirectory) close() error {
	if d == nil {
		return nil
	}
	var childErr, parentErr error
	if d.root != nil {
		childErr = d.root.Close()
		d.root = nil
	}
	if d.parent != nil {
		parentErr = d.parent.Close()
		d.parent = nil
	}
	return errors.Join(childErr, parentErr)
}

func (d *artworkDirectory) removeCreated() error {
	if d == nil || !d.created || d.parent == nil {
		return nil
	}
	info, err := d.parent.Lstat("artwork")
	if errors.Is(err, os.ErrNotExist) {
		d.created = false
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || !os.SameFile(info, d.info) {
		return errors.New("artwork rollback identity changed")
	}
	if err := d.parent.Remove("artwork"); err != nil {
		return err
	}
	d.created = false
	return nil
}

func (d *artworkDirectory) removeIfEmpty() error {
	if d == nil || d.root == nil || d.parent == nil {
		return nil
	}
	if err := d.intact(); err != nil {
		return err
	}
	entries, err := d.readDir()
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("artwork directory is not empty")
	}
	return d.parent.Remove("artwork")
}

func NewArtworkFetcher(config ArtworkFetcherConfig) (*ArtworkFetcher, error) {
	owner, err := acquirePrivateRoot(config.Root, true)
	if err != nil {
		return nil, newOpError(ErrStorage, err)
	}
	artworkRoot, err := openArtworkDirectory(owner, true)
	if err != nil {
		_ = owner.abort(true)
		return nil, newOpError(ErrStorage, err)
	}
	if err := owner.commit(); err != nil {
		_ = artworkRoot.removeCreated()
		_ = artworkRoot.close()
		_ = owner.abort(true)
		return nil, newOpError(ErrStorage, err)
	}
	return &ArtworkFetcher{root: owner.path, artwork: filepath.Join(owner.path, "artwork"), metadataRoot: owner.root, artworkRoot: artworkRoot, client: newArtworkHTTPClient(config.HTTPClient)}, nil
}

func newArtworkHTTPClient(configured *http.Client) *http.Client {
	if configured == nil {
		return NewHardenedHTTPClient()
	}
	copy := *configured
	copy.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	copy.Jar = nil
	copy.Timeout = 0
	return &copy
}

func newArtworkFetcherFromCache(cache *Cache, config ArtworkFetcherConfig) (*ArtworkFetcher, error) {
	if cache == nil || cache.sqliteRoot == nil {
		return nil, errors.New("metadata cache root is unavailable")
	}
	clone, err := cache.sqliteRoot.OpenRoot(".")
	if err != nil {
		return nil, err
	}
	info, err := clone.Stat(".")
	if err != nil {
		_ = clone.Close()
		return nil, err
	}
	owner := &privateRoot{path: cache.root, root: clone, info: info}
	artworkRoot, err := openArtworkDirectory(owner, false)
	if err != nil {
		_ = clone.Close()
		return nil, err
	}
	return &ArtworkFetcher{root: cache.root, artwork: filepath.Join(cache.root, "artwork"), metadataRoot: clone, artworkRoot: artworkRoot, client: newArtworkHTTPClient(config.HTTPClient)}, nil
}

func ArtworkURL(role ArtworkRole, imageID string) (string, error) {
	if role != ArtworkCover && role != ArtworkBackdrop {
		return "", newOpError(ErrPolicyBlocked, nil)
	}
	if !imageIDPattern.MatchString(imageID) {
		return "", newOpError(ErrPolicyBlocked, nil)
	}
	transform := artworkTransformCover
	if role == ArtworkBackdrop {
		transform = artworkTransformBackdrop
	}
	path := "/igdb/image/upload/" + transform + "/" + imageID + ".jpg"
	parsed := url.URL{Scheme: "https", Host: "images.igdb.com", Path: path}
	return parsed.String(), nil
}

func (f *ArtworkFetcher) Fetch(ctx context.Context, cacheKey []byte, reference ArtworkRef) (ArtworkObject, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := f.isClosed(); err != nil {
		return ArtworkObject{}, err
	}
	rawURL, err := ArtworkURL(reference.Role, reference.ID)
	if err != nil {
		return ArtworkObject{}, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return ArtworkObject{}, newOpError(ErrPolicyBlocked, nil)
	}
	request.Header.Set("Accept", "image/jpeg, image/png")
	response, err := f.client.Do(request)
	if err != nil {
		return ArtworkObject{}, mapContextError(err)
	}
	if response == nil || response.Body == nil {
		return ArtworkObject{}, newOpError(ErrInvalidResponse, nil)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ArtworkObject{}, newOpError(ErrUpstreamUnavailable, nil)
	}
	declared, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || (declared != "image/jpeg" && declared != "image/png") || response.Header.Get("Content-Type") != declared {
		return ArtworkObject{}, newOpError(ErrInvalidResponse, err)
	}
	compressed, err := readLimited(response.Body, maxArtworkCompressed)
	if err != nil {
		return ArtworkObject{}, newOpError(ErrInvalidResponse, err)
	}
	sanitized, width, height, err := sanitizeImage(declared, compressed)
	if err != nil {
		return ArtworkObject{}, newOpError(ErrInvalidResponse, err)
	}
	object := ArtworkObject{
		Handle:  artworkHandle(cacheKey, reference.Role, reference.ID),
		Digest:  digestBytes(sanitized),
		MIME:    declared,
		Size:    int64(len(sanitized)),
		Width:   width,
		Height:  height,
		Created: time.Now().UnixNano(),
	}
	if object.Size > maxArtworkCompressed {
		return ArtworkObject{}, newOpError(ErrInvalidResponse, nil)
	}
	if err := f.isClosed(); err != nil {
		return ArtworkObject{}, err
	}
	if err := requestCtx.Err(); err != nil {
		return ArtworkObject{}, mapContextError(err)
	}
	if err := f.publish(object.Digest, sanitized); err != nil {
		return ArtworkObject{}, newOpError(ErrStorage, err)
	}
	return object, nil
}

func (f *ArtworkFetcher) OpenDigest(digest string) (Artwork, error) {
	if !digestPattern.MatchString(digest) {
		return Artwork{}, newOpError(ErrPolicyBlocked, nil)
	}
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if err := f.isClosed(); err != nil {
		return Artwork{}, err
	}
	if err := f.artworkRoot.intact(); err != nil {
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	info, err := f.artworkRoot.lstat(digest)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o600 {
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	file, err := f.artworkRoot.open(digest)
	if err != nil {
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	if err := verifyDigestFile(file, digest); err != nil {
		_ = file.Close()
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	if err := f.artworkRoot.beforeUse(); err != nil {
		_ = file.Close()
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		_ = file.Close()
		return Artwork{}, newOpError(ErrStorage, nil)
	}
	return Artwork{MIME: detectStoredMIME(file), Size: info.Size(), Reader: file}, nil
}

func detectStoredMIME(file *os.File) string {
	var header [512]byte
	count, err := file.ReadAt(header[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return "application/octet-stream"
	}
	return http.DetectContentType(header[:count])
}

func (f *ArtworkFetcher) Close() error {
	if f == nil {
		return nil
	}
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	f.closedMu.Lock()
	if f.closed {
		f.closedMu.Unlock()
		return nil
	}
	f.closed = true
	f.closedMu.Unlock()
	if f.client != nil {
		if transport, ok := f.client.Transport.(interface{ CloseIdleConnections() }); ok {
			transport.CloseIdleConnections()
		}
	}
	var artworkErr, metadataErr error
	if f.artworkRoot != nil {
		artworkErr = f.artworkRoot.close()
	}
	if f.metadataRoot != nil {
		metadataErr = f.metadataRoot.Close()
		f.metadataRoot = nil
	}
	return errors.Join(artworkErr, metadataErr)
}

func (f *ArtworkFetcher) isClosed() error {
	f.closedMu.RLock()
	defer f.closedMu.RUnlock()
	if f.closed {
		return newOpError(ErrCanceled, context.Canceled)
	}
	return nil
}

func sanitizeImage(declared string, compressed []byte) ([]byte, int, int, error) {
	if len(compressed) == 0 {
		return nil, 0, 0, errors.New("empty image")
	}
	sniffed := http.DetectContentType(compressed[:minInt(len(compressed), 512)])
	if sniffed != declared {
		return nil, 0, 0, errors.New("image MIME does not match content")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(compressed))
	if err != nil || format != imageFormat(declared) {
		return nil, 0, 0, errors.New("image configuration is invalid")
	}
	if err := validateImageTermination(declared, compressed); err != nil {
		return nil, 0, 0, err
	}
	if config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16_777_216 {
		return nil, 0, 0, errors.New("image dimensions exceed policy")
	}
	decoded, format, err := image.Decode(bytes.NewReader(compressed))
	if err != nil || format != imageFormat(declared) || decoded.Bounds().Dx() != config.Width || decoded.Bounds().Dy() != config.Height {
		return nil, 0, 0, errors.New("image decode is invalid")
	}
	var output bytes.Buffer
	switch declared {
	case "image/png":
		err = png.Encode(&output, decoded)
	case "image/jpeg":
		err = jpeg.Encode(&output, decoded, &jpeg.Options{Quality: 90})
	default:
		err = errors.New("image MIME is not allowed")
	}
	if err != nil || output.Len() > maxArtworkCompressed {
		return nil, 0, 0, errors.New("sanitized image exceeds policy")
	}
	return output.Bytes(), config.Width, config.Height, nil
}

func imageFormat(mime string) string {
	if mime == "image/png" {
		return "png"
	}
	return "jpeg"
}

func validateImageTermination(declared string, compressed []byte) error {
	switch declared {
	case "image/png":
		if len(compressed) < 8 || !bytes.Equal(compressed[:8], []byte{137, 80, 78, 71, 13, 10, 26, 10}) {
			return errors.New("PNG signature is invalid")
		}
		offset := 8
		for offset < len(compressed) {
			if len(compressed)-offset < 12 {
				return errors.New("PNG chunk is truncated")
			}
			length := int(binary.BigEndian.Uint32(compressed[offset : offset+4]))
			if length > len(compressed)-offset-12 {
				return errors.New("PNG chunk exceeds image bounds")
			}
			chunkType := string(compressed[offset+4 : offset+8])
			offset += 12 + length
			if chunkType == "IEND" {
				if length != 0 || offset != len(compressed) {
					return errors.New("PNG has trailing active content")
				}
				return nil
			}
		}
		return errors.New("PNG has no IEND chunk")
	case "image/jpeg":
		if len(compressed) < 4 || compressed[0] != 0xff || compressed[1] != 0xd8 {
			return errors.New("JPEG signature is invalid")
		}
		marker := bytes.Index(compressed[2:], []byte{0xff, 0xd9})
		if marker < 0 || marker+4 != len(compressed) {
			return errors.New("JPEG has trailing active content")
		}
		return nil
	default:
		return errors.New("image MIME is not allowed")
	}
}

func artworkHandle(cacheKey []byte, role ArtworkRole, imageID string) string {
	hash := sha256.New()
	_, _ = hash.Write(cacheKey)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(role))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(imageID))
	_, _ = hash.Write([]byte{0})
	if role == ArtworkCover {
		_, _ = hash.Write([]byte(artworkTransformCover))
	} else {
		_, _ = hash.Write([]byte(artworkTransformBackdrop))
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func digestBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (f *ArtworkFetcher) publish(digest string, content []byte) error {
	f.writeMu.Lock()
	defer f.writeMu.Unlock()
	if err := f.isClosed(); err != nil {
		return err
	}
	if err := f.artworkRoot.intact(); err != nil {
		return err
	}
	if info, err := f.artworkRoot.lstat(digest); err == nil {
		if info.Mode().Perm() != 0o600 || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() != int64(len(content)) {
			return errors.New("existing artwork object failed validation")
		}
		file, err := f.artworkRoot.open(digest)
		if err != nil {
			return err
		}
		verifyErr := verifyDigestFile(file, digest)
		closeErr := file.Close()
		if verifyErr != nil {
			return verifyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return f.artworkRoot.beforeUse()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	temporary := ".tmp-" + hex.EncodeToString(random)
	file, err := f.artworkRoot.openFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = f.artworkRoot.remove(temporary)
		}
	}()
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := f.artworkRoot.beforeUse(); err != nil {
		return err
	}
	if err := f.artworkRoot.rename(temporary, digest); err != nil {
		return err
	}
	cleanup = false
	directory, err := f.artworkRoot.open(".")
	if err != nil {
		return err
	}
	err = directory.Sync()
	_ = directory.Close()
	return err
}

func verifyDigestFile(file *os.File, expected string) error {
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != expected {
		return fmt.Errorf("artwork digest mismatch")
	}
	return nil
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
