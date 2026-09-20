package rooms

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

const (
	imageTypeName = "rooms.image"
	coverPrefix   = "cover:"
	assetPrefix   = "asset:"
)

// Image is a script-visible bitmap handle. Key is "asset:<path>" for pack
// files and "cover:<gameID>" for catalog artwork the launcher fetches.
type Image struct {
	Key   string
	Ready bool
	Err   string
	W, H  int
	Img   *image.RGBA

	requested bool
}

// IsCover reports whether the handle refers to catalog artwork.
func (im *Image) IsCover() bool { return strings.HasPrefix(im.Key, coverPrefix) }

// GameID is the catalog id behind a cover handle.
func (im *Image) GameID() string { return strings.TrimPrefix(im.Key, coverPrefix) }

func (r *Instance) installImage() *lua.LTable {
	L := r.L
	mt := L.NewTypeMetatable(imageTypeName)
	L.SetField(mt, "__index", L.NewFunction(r.imageIndex))
	L.SetField(mt, "__tostring", L.NewFunction(func(L *lua.LState) int {
		im := r.checkImage(L, 1)
		L.Push(lua.LString(im.Key))
		return 1
	}))
	t := L.NewTable()
	L.SetFuncs(t, map[string]lua.LGFunction{
		"load":  r.imageLoad,
		"cover": r.imageCover,
	})
	return t
}

func (r *Instance) checkImage(L *lua.LState, idx int) *Image {
	ud := L.CheckUserData(idx)
	if im, ok := ud.Value.(*Image); ok {
		return im
	}
	L.ArgError(idx, "image expected")
	return nil
}

func (r *Instance) imageIndex(L *lua.LState) int {
	im := r.checkImage(L, 1)
	switch L.CheckString(2) {
	case "ready":
		L.Push(lua.LBool(im.Ready && im.Err == ""))
	case "w", "width":
		L.Push(lua.LNumber(im.W))
	case "h", "height":
		L.Push(lua.LNumber(im.H))
	case "err", "error":
		if im.Err == "" {
			L.Push(lua.LNil)
		} else {
			L.Push(lua.LString(im.Err))
		}
	case "key":
		L.Push(lua.LString(im.Key))
	default:
		L.Push(lua.LNil)
	}
	return 1
}

func (r *Instance) handle(L *lua.LState, key string) (*Image, *lua.LUserData) {
	im, ok := r.images[key]
	if !ok {
		im = &Image{Key: key}
		r.images[key] = im
	}
	ud := L.NewUserData()
	ud.Value = im
	L.SetMetatable(ud, L.GetTypeMetatable(imageTypeName))
	return im, ud
}

// image.load("assets/bg.png") decodes a pack file on an engine goroutine.
func (r *Instance) imageLoad(L *lua.LState) int {
	rel := L.CheckString(1)
	clean, err := cleanPackPath(rel)
	if err != nil {
		L.ArgError(1, err.Error())
	}
	im, ud := r.handle(L, assetPrefix+clean)
	if !im.requested {
		im.requested = true
		go r.decodeAsset(im.Key, clean)
	}
	L.Push(ud)
	return 1
}

// image.cover(game_id) asks the launcher for catalog artwork.
func (r *Instance) imageCover(L *lua.LState) int {
	id := strings.TrimSpace(L.CheckString(1))
	if id == "" {
		L.ArgError(1, "game id required")
	}
	_, ud := r.handle(L, coverPrefix+id)
	L.Push(ud)
	return 1
}

func (r *Instance) decodeAsset(key, clean string) {
	data, err := readPackFile(r.pack.FS, clean, maxAssetBytes)
	if err != nil {
		r.deliver(asyncResult{imageKey: key, err: err})
		return
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		r.deliver(asyncResult{imageKey: key, err: fmt.Errorf("decode %s: %w", clean, err)})
		return
	}
	r.deliver(asyncResult{imageKey: key, img: toRGBA(src)})
}

func toRGBA(src image.Image) *image.RGBA {
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	b := src.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), src, b.Min, draw.Src)
	return out
}

func (r *Instance) applyImage(res asyncResult) {
	im, ok := r.images[res.imageKey]
	if !ok {
		return
	}
	if res.err != nil {
		im.Ready = true
		im.Err = res.err.Error()
		return
	}
	if res.img == nil {
		im.Ready = true
		im.Err = "no image"
		return
	}
	im.Img = res.img
	im.W = res.img.Bounds().Dx()
	im.H = res.img.Bounds().Dy()
	im.Ready = true
}

// TakeCoverRequests returns game ids whose artwork the script wants and the
// launcher has not yet been asked for. The launcher answers with DeliverCover.
func (r *Instance) TakeCoverRequests() []string {
	var ids []string
	for key, im := range r.images {
		if im.requested || !strings.HasPrefix(key, coverPrefix) {
			continue
		}
		im.requested = true
		ids = append(ids, im.GameID())
	}
	return ids
}

// DeliverCover completes a cover request. Safe to call from any goroutine; a
// nil img with nil err marks the cover as missing.
func (r *Instance) DeliverCover(gameID string, img *image.RGBA, err error) {
	if err == nil && img == nil {
		err = fmt.Errorf("no artwork")
	}
	r.deliver(asyncResult{imageKey: coverPrefix + gameID, img: img, err: err})
}

// Images returns every decoded bitmap by handle key for texture upload.
func (r *Instance) Images() map[string]*image.RGBA {
	out := make(map[string]*image.RGBA, len(r.images))
	for key, im := range r.images {
		if im.Img != nil {
			out[key] = im.Img
		}
	}
	return out
}
