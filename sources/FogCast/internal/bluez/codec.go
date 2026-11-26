// SPDX-License-Identifier: GPL-3.0-or-later

// Package bluez provides a zero-dependency D-Bus client for querying and
// managing BlueZ adapters and paired/connected controllers on Linux.
package bluez

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

const (
	msgMethodCall   = 1
	msgMethodReturn = 2
	msgError        = 3
	msgSignal       = 4

	fieldPath        = 1
	fieldInterface   = 2
	fieldMember      = 3
	fieldError       = 4
	fieldReplySerial = 5
	fieldDest        = 6
	fieldSender      = 7
	fieldSignature   = 8

	maxMessage = 1 << 20
)

type message struct {
	Type        byte
	Flags       byte
	Serial      uint32
	ReplySerial uint32
	Path        string
	Iface       string
	Member      string
	ErrorName   string
	Dest        string
	Sender      string
	Signature   string
	Body        []byte
}

type adapterProps struct {
	Path        string
	Name        string
	Powered     bool
	Discovering bool
}

type deviceProps struct {
	Path      string
	Adapter   string
	Address   string
	Name      string
	Modalias  string
	Class     uint32
	UUIDs     []string
	Paired    bool
	Connected bool
}

type encoder struct{ buf []byte }

func (e *encoder) pad(n int) {
	for len(e.buf)%n != 0 {
		e.buf = append(e.buf, 0)
	}
}

func (e *encoder) u32(v uint32) {
	e.pad(4)
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	e.buf = append(e.buf, b[:]...)
}

func (e *encoder) str(s string) {
	e.u32(uint32(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *encoder) sig(s string) {
	e.buf = append(e.buf, byte(len(s)))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0)
}

func (e *encoder) boolean(v bool) {
	if v {
		e.u32(1)
		return
	}
	e.u32(0)
}

func (e *encoder) array(align int, write func()) {
	e.pad(4)
	pos := len(e.buf)
	e.buf = append(e.buf, 0, 0, 0, 0)
	e.pad(align)
	start := len(e.buf)
	write()
	binary.LittleEndian.PutUint32(e.buf[pos:pos+4], uint32(len(e.buf)-start))
}

func (e *encoder) field(code byte, kind string, write func()) {
	e.pad(8)
	e.buf = append(e.buf, code)
	e.sig(kind)
	write()
}

func bodyString(value string) []byte {
	e := &encoder{}
	e.str(value)
	return e.buf
}

func bodyPath(path string) []byte {
	e := &encoder{}
	e.str(path)
	return e.buf
}

func bodyPropBool(iface, name string, flag bool) []byte {
	e := &encoder{}
	e.str(iface)
	e.str(name)
	e.sig("b")
	e.boolean(flag)
	return e.buf
}

func writeMessage(w io.Writer, m *message, body []byte) error {
	if body == nil {
		body = []byte{}
	}
	if len(body) > maxMessage {
		return errors.New("dbus body is too large")
	}
	e := &encoder{}
	e.buf = append(e.buf, 'l', m.Type, m.Flags, 1)
	e.u32(uint32(len(body)))
	e.u32(m.Serial)
	e.array(8, func() {
		if m.Path != "" {
			e.field(fieldPath, "o", func() { e.str(m.Path) })
		}
		if m.Iface != "" {
			e.field(fieldInterface, "s", func() { e.str(m.Iface) })
		}
		if m.Member != "" {
			e.field(fieldMember, "s", func() { e.str(m.Member) })
		}
		if m.ErrorName != "" {
			e.field(fieldError, "s", func() { e.str(m.ErrorName) })
		}
		if m.ReplySerial != 0 {
			e.field(fieldReplySerial, "u", func() { e.u32(m.ReplySerial) })
		}
		if m.Dest != "" {
			e.field(fieldDest, "s", func() { e.str(m.Dest) })
		}
		if m.Sender != "" {
			e.field(fieldSender, "s", func() { e.str(m.Sender) })
		}
		if m.Signature != "" {
			e.field(fieldSignature, "g", func() { e.sig(m.Signature) })
		}
	})
	e.pad(8)
	e.buf = append(e.buf, body...)
	_, err := w.Write(e.buf)
	return err
}

type decoder struct {
	b []byte
	i int
}

func (d *decoder) pad(n int) error {
	for d.i%n != 0 {
		if d.i >= len(d.b) {
			return io.ErrUnexpectedEOF
		}
		d.i++
	}
	return nil
}

func (d *decoder) take(n int) ([]byte, error) {
	if n < 0 || d.i+n > len(d.b) {
		return nil, io.ErrUnexpectedEOF
	}
	b := d.b[d.i : d.i+n]
	d.i += n
	return b, nil
}

func (d *decoder) u32() (uint32, error) {
	if err := d.pad(4); err != nil {
		return 0, err
	}
	b, err := d.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (d *decoder) str() (string, error) {
	n, err := d.u32()
	if err != nil {
		return "", err
	}
	if n > maxMessage {
		return "", errors.New("dbus string is too long")
	}
	b, err := d.take(int(n))
	if err != nil {
		return "", err
	}
	nul, err := d.take(1)
	if err != nil || nul[0] != 0 {
		return "", errors.New("dbus string is missing its terminator")
	}
	return string(b), nil
}

func (d *decoder) sig() (string, error) {
	if d.i >= len(d.b) {
		return "", io.ErrUnexpectedEOF
	}
	n := int(d.b[d.i])
	d.i++
	b, err := d.take(n)
	if err != nil {
		return "", err
	}
	nul, err := d.take(1)
	if err != nil || nul[0] != 0 {
		return "", errors.New("dbus signature is missing its terminator")
	}
	return string(b), nil
}

func splitOne(sig string) (string, string, error) {
	if sig == "" {
		return "", "", errors.New("empty dbus signature")
	}
	switch sig[0] {
	case 'a':
		elem, rest, err := splitOne(sig[1:])
		if err != nil {
			return "", "", err
		}
		return "a" + elem, rest, nil
	case '(', '{':
		closer := byte(')')
		if sig[0] == '{' {
			closer = '}'
		}
		depth := 0
		for i := 0; i < len(sig); i++ {
			switch sig[i] {
			case sig[0]:
				depth++
			case closer:
				depth--
				if depth == 0 {
					return sig[:i+1], sig[i+1:], nil
				}
			}
		}
		return "", "", fmt.Errorf("unclosed dbus signature %q", sig)
	default:
		if strings.ContainsRune("ybnqiuxtdsgov", rune(sig[0])) {
			return sig[:1], sig[1:], nil
		}
		return "", "", fmt.Errorf("unknown dbus type %q", sig[:1])
	}
}

func alignOf(sig string) int {
	one, _, err := splitOne(sig)
	if err != nil || one == "" {
		return 1
	}
	switch one[0] {
	case 'y', 'g':
		return 1
	case 'n', 'q':
		return 2
	case '(', '{', 'x', 't', 'd':
		return 8
	default:
		return 4
	}
}

func (d *decoder) readComplete(sig string) (any, string, error) {
	one, rest, err := splitOne(sig)
	if err != nil {
		return nil, "", err
	}
	v, err := d.readOne(one)
	if err != nil {
		return nil, "", err
	}
	return v, rest, nil
}

func (d *decoder) readSig(sig string) ([]any, error) {
	var out []any
	for sig != "" {
		v, rest, err := d.readComplete(sig)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		sig = rest
	}
	return out, nil
}

func (d *decoder) readOne(one string) (any, error) {
	switch one[0] {
	case 'y':
		b, err := d.take(1)
		if err != nil {
			return nil, err
		}
		return b[0], nil
	case 'b':
		v, err := d.u32()
		if err != nil {
			return nil, err
		}
		if v > 1 {
			return nil, errors.New("dbus bool is not 0 or 1")
		}
		return v == 1, nil
	case 'n', 'q':
		if err := d.pad(2); err != nil {
			return nil, err
		}
		b, err := d.take(2)
		if err != nil {
			return nil, err
		}
		v := binary.LittleEndian.Uint16(b)
		if one[0] == 'n' {
			return int16(v), nil
		}
		return v, nil
	case 'i':
		v, err := d.u32()
		if err != nil {
			return nil, err
		}
		return int32(v), nil
	case 'u':
		return d.u32()
	case 'x', 't', 'd':
		if err := d.pad(8); err != nil {
			return nil, err
		}
		b, err := d.take(8)
		if err != nil {
			return nil, err
		}
		v := binary.LittleEndian.Uint64(b)
		switch one[0] {
		case 'x':
			return int64(v), nil
		case 'd':
			return math.Float64frombits(v), nil
		default:
			return v, nil
		}
	case 's', 'o':
		return d.str()
	case 'g':
		return d.sig()
	case 'v':
		kind, err := d.sig()
		if err != nil {
			return nil, err
		}
		if kind == "" {
			return nil, errors.New("empty dbus variant")
		}
		v, rest, err := d.readComplete(kind)
		if err != nil {
			return nil, err
		}
		if rest != "" {
			return nil, fmt.Errorf("dbus variant has trailing signature %s", rest)
		}
		return v, nil
	case 'a':
		n, err := d.u32()
		if err != nil {
			return nil, err
		}
		elem := one[1:]
		if err := d.pad(alignOf(elem)); err != nil {
			return nil, err
		}
		if d.i > len(d.b) || int(n) > len(d.b)-d.i {
			return nil, io.ErrUnexpectedEOF
		}
		end := d.i + int(n)
		var items []any
		for d.i < end {
			v, err := d.readOne(elem)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
			if d.i > end {
				return nil, errors.New("dbus array overruns its length")
			}
		}
		if d.i != end {
			return nil, errors.New("dbus array underruns its length")
		}
		return items, nil
	case '(', '{':
		closer := byte(')')
		if one[0] == '{' {
			closer = '}'
		}
		if len(one) < 2 || one[len(one)-1] != closer {
			return nil, errors.New("truncated dbus container")
		}
		if err := d.pad(8); err != nil {
			return nil, err
		}
		return d.readSig(one[1 : len(one)-1])
	default:
		return nil, fmt.Errorf("unsupported dbus type %q", one)
	}
}

func readMessage(r io.Reader) (*message, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	if hdr[0] != 'l' || hdr[3] != 1 {
		return nil, errors.New("dbus message is not little-endian version 1")
	}
	bodyLen := binary.LittleEndian.Uint32(hdr[4:8])
	fieldsLen := binary.LittleEndian.Uint32(hdr[12:16])
	if bodyLen > maxMessage || fieldsLen > maxMessage {
		return nil, errors.New("dbus message is too large")
	}
	fields := make([]byte, fieldsLen)
	if _, err := io.ReadFull(r, fields); err != nil {
		return nil, err
	}
	consumed := 16 + int(fieldsLen)
	if pad := (8 - consumed%8) % 8; pad > 0 {
		var discard [8]byte
		if _, err := io.ReadFull(r, discard[:pad]); err != nil {
			return nil, err
		}
	}
	body := make([]byte, bodyLen)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	msg, err := parseHeader(fields)
	if err != nil {
		return nil, err
	}
	msg.Type = hdr[1]
	msg.Flags = hdr[2]
	msg.Serial = binary.LittleEndian.Uint32(hdr[8:12])
	msg.Body = body
	return msg, nil
}

func parseHeader(fields []byte) (*message, error) {
	d := &decoder{b: fields}
	msg := &message{}
	for d.i < len(fields) {
		if err := d.pad(8); err != nil {
			return nil, err
		}
		if d.i >= len(fields) {
			break
		}
		codeb, err := d.take(1)
		if err != nil {
			return nil, err
		}
		kind, err := d.sig()
		if err != nil {
			return nil, err
		}
		val, rest, err := d.readComplete(kind)
		if err != nil {
			return nil, err
		}
		if rest != "" {
			return nil, errors.New("dbus header field has a compound signature")
		}
		switch codeb[0] {
		case fieldPath, fieldInterface, fieldMember, fieldError, fieldDest, fieldSender:
			s, ok := val.(string)
			if !ok {
				return nil, errors.New("dbus header string has the wrong type")
			}
			switch codeb[0] {
			case fieldPath:
				msg.Path = s
			case fieldInterface:
				msg.Iface = s
			case fieldMember:
				msg.Member = s
			case fieldError:
				msg.ErrorName = s
			case fieldDest:
				msg.Dest = s
			case fieldSender:
				msg.Sender = s
			}
		case fieldReplySerial:
			n, ok := val.(uint32)
			if !ok {
				return nil, errors.New("dbus reply serial has the wrong type")
			}
			msg.ReplySerial = n
		case fieldSignature:
			s, ok := val.(string)
			if !ok {
				return nil, errors.New("dbus signature field has the wrong type")
			}
			msg.Signature = s
		}
	}
	return msg, nil
}

func decodeStringResult(body []byte) (string, error) {
	d := &decoder{b: body}
	v, rest, err := d.readComplete("s")
	if err != nil || rest != "" {
		return "", errors.New("dbus reply is not a string")
	}
	s, ok := v.(string)
	if !ok {
		return "", errors.New("dbus reply string has wrong type")
	}
	return s, nil
}

func decodeManagedObjects(body []byte) (map[string]adapterProps, map[string]deviceProps, error) {
	d := &decoder{b: body}
	v, rest, err := d.readComplete("a{oa{sa{sv}}}")
	if err != nil || rest != "" {
		if err == nil {
			err = errors.New("trailing managed objects")
		}
		return nil, nil, err
	}
	entries, ok := v.([]any)
	if !ok {
		return nil, nil, errors.New("managed objects")
	}
	adapters := map[string]adapterProps{}
	devices := map[string]deviceProps{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, nil, errors.New("managed object entry")
		}
		objPath, ok := pair[0].(string)
		if !ok {
			return nil, nil, errors.New("managed object path")
		}
		ifaces, err := ifaceMap(pair[1])
		if err != nil {
			return nil, nil, err
		}
		if props, ok := ifaces[adapterIface]; ok {
			ad := adapterProps{Path: objPath}
			applyAdapterProps(&ad, props)
			adapters[objPath] = ad
		}
		if props, ok := ifaces[deviceIface]; ok {
			dev := deviceProps{Path: objPath}
			applyDeviceProps(&dev, props)
			devices[objPath] = dev
		}
	}
	return adapters, devices, nil
}

func decodeAdded(body []byte) (string, map[string]map[string]any, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("oa{sa{sv}}")
	if err != nil || len(vals) != 2 {
		if err == nil {
			err = errors.New("InterfacesAdded")
		}
		return "", nil, err
	}
	objPath, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("InterfacesAdded path")
	}
	ifaces, err := ifaceMap(vals[1])
	if err != nil {
		return "", nil, err
	}
	return objPath, ifaces, nil
}

func decodeRemoved(body []byte) (string, []string, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("oas")
	if err != nil || len(vals) != 2 {
		return "", nil, errors.New("InterfacesRemoved")
	}
	objPath, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("InterfacesRemoved path")
	}
	list, ok := vals[1].([]any)
	if !ok {
		return "", nil, errors.New("InterfacesRemoved interfaces")
	}
	var names []string
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return "", nil, errors.New("InterfacesRemoved name")
		}
		names = append(names, s)
	}
	return objPath, names, nil
}

func decodeChanged(body []byte) (string, map[string]any, error) {
	d := &decoder{b: body}
	vals, err := d.readSig("sa{sv}as")
	if err != nil || len(vals) < 2 {
		return "", nil, errors.New("PropertiesChanged")
	}
	iface, ok := vals[0].(string)
	if !ok {
		return "", nil, errors.New("PropertiesChanged interface")
	}
	props, err := propMap(vals[1])
	if err != nil {
		return "", nil, err
	}
	return iface, props, nil
}

func decodeNameOwnerChanged(body []byte) (string, string, string, error) {
	d := &decoder{b: body}
	values, err := d.readSig("sss")
	if err != nil || len(values) != 3 {
		return "", "", "", errors.New("NameOwnerChanged")
	}
	name, nameOK := values[0].(string)
	oldOwner, oldOK := values[1].(string)
	newOwner, newOK := values[2].(string)
	if !nameOK || !oldOK || !newOK || name == "" {
		return "", "", "", errors.New("NameOwnerChanged fields")
	}
	return name, oldOwner, newOwner, nil
}

func ifaceMap(v any) (map[string]map[string]any, error) {
	entries, ok := v.([]any)
	if !ok {
		return nil, errors.New("dbus interfaces")
	}
	out := map[string]map[string]any{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("dbus interface entry")
		}
		name, ok := pair[0].(string)
		if !ok {
			return nil, errors.New("dbus interface name")
		}
		props, err := propMap(pair[1])
		if err != nil {
			return nil, err
		}
		out[name] = props
	}
	return out, nil
}

func propMap(v any) (map[string]any, error) {
	entries, ok := v.([]any)
	if !ok {
		return nil, errors.New("dbus properties")
	}
	out := map[string]any{}
	for _, ent := range entries {
		pair, ok := ent.([]any)
		if !ok || len(pair) != 2 {
			return nil, errors.New("dbus property entry")
		}
		name, ok := pair[0].(string)
		if !ok {
			return nil, errors.New("dbus property name")
		}
		out[name] = pair[1]
	}
	return out, nil
}

func applyAdapterProps(ad *adapterProps, props map[string]any) {
	if s, ok := props["Name"].(string); ok && s != "" && ad.Name == "" {
		ad.Name = s
	}
	if s, ok := props["Alias"].(string); ok && s != "" {
		ad.Name = s
	}
	if b, ok := props["Powered"].(bool); ok {
		ad.Powered = b
	}
	if b, ok := props["Discovering"].(bool); ok {
		ad.Discovering = b
	}
}

func applyDeviceProps(dev *deviceProps, props map[string]any) {
	if s, ok := props["Address"].(string); ok && s != "" {
		dev.Address = s
	}
	if s, ok := props["Adapter"].(string); ok && s != "" {
		dev.Adapter = s
	}
	if s, ok := props["Name"].(string); ok && s != "" && dev.Name == "" {
		dev.Name = s
	}
	if s, ok := props["Alias"].(string); ok && s != "" {
		dev.Name = s
	}
	if s, ok := props["Modalias"].(string); ok {
		dev.Modalias = s
	}
	if u, ok := props["Class"].(uint32); ok {
		dev.Class = u
	}
	if b, ok := props["Paired"].(bool); ok {
		dev.Paired = b
	}
	if b, ok := props["Connected"].(bool); ok {
		dev.Connected = b
	}
	if list, ok := props["UUIDs"].([]any); ok {
		dev.UUIDs = nil
		for _, item := range list {
			if s, ok := item.(string); ok {
				dev.UUIDs = append(dev.UUIDs, s)
			}
		}
	}
}
