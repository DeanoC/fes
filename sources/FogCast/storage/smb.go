package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/hirochachacha/go-smb2"
)

// Every operation owns its connection. Cancellation closes the socket, including
// when a server stops answering during authentication or a file read.
type smbConnection struct {
	conn    net.Conn
	session *smb2.Session
	share   *smb2.Share
	stop    func() bool
	once    sync.Once
}

func connectSMB(ctx context.Context, d Drive) (*smbConnection, error) {
	parts := strings.Split(strings.TrimPrefix(d.Source, "//"), "/")
	if len(parts) != 2 {
		return nil, errors.New("invalid SMB share")
	}
	address := parts[0]
	if _, _, err := net.SplitHostPort(address); err != nil {
		address = net.JoinHostPort(strings.Trim(address, "[]"), "445")
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, errors.New("SMB server is unavailable")
	}
	c := &smbConnection{conn: conn}
	c.stop = context.AfterFunc(ctx, func() { conn.Close() })
	// Guest is an explicit NTLM guest account, rather than an anonymous session.
	user := d.Username
	if user == "" {
		user = "guest"
	}
	dialer := &smb2.Dialer{Initiator: &smb2.NTLMInitiator{User: user, Password: d.Password, Domain: d.Domain}}
	session, err := dialer.DialContext(ctx, conn)
	if err != nil {
		c.Close()
		return nil, errors.New("SMB login failed; check username, password and domain")
	}
	c.session = session.WithContext(ctx)
	c.share, err = c.session.Mount(parts[1])
	if err != nil {
		c.Close()
		return nil, errors.New("SMB share is unavailable for this account")
	}
	c.share = c.share.WithContext(ctx)
	return c, nil
}
func (c *smbConnection) Close() error {
	c.once.Do(func() {
		if c.stop != nil {
			c.stop()
		}
		// Close transport first: cleanup must not wait for an unresponsive server.
		c.conn.Close()
		if c.share != nil {
			_ = c.share.Umount()
		}
		if c.session != nil {
			_ = c.session.Logoff()
		}
	})
	return nil
}
func smbEntries(ctx context.Context, d Drive, rel string) ([]Entry, error) {
	c, err := connectSMB(ctx, d)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	infos, err := c.share.ReadDir(strings.ReplaceAll(rel, "/", `\`))
	if err != nil {
		return nil, errors.New("SMB folder is unavailable")
	}
	out := []Entry{}
	for _, info := range infos {
		if strings.HasPrefix(info.Name(), ".") || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() || info.Mode().IsRegular() {
			out = append(out, Entry{Name: info.Name(), Directory: info.IsDir(), Size: info.Size()})
		}
	}
	sortEntries(out)
	return out, nil
}

type smbFile struct {
	*smb2.File
	connection *smbConnection
}

func (f *smbFile) Close() error {
	// File.Close uses the share context; closing transport bounds it on disconnect.
	f.connection.conn.SetDeadline(time.Now().Add(time.Second))
	err := f.File.Close()
	_ = f.connection.Close()
	return err
}
func smbOpen(ctx context.Context, d Drive, rel string, limit int64) (io.ReadCloser, int64, error) {
	c, err := connectSMB(ctx, d)
	if err != nil {
		return nil, 0, err
	}
	file, err := c.share.Open(strings.ReplaceAll(rel, "/", `\`))
	if err != nil {
		c.Close()
		return nil, 0, errors.New("SMB file is unavailable")
	}
	info, err := file.Stat()
	if err != nil {
		c.Close()
		return nil, 0, errors.New("SMB file metadata is unavailable")
	}
	if !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > limit {
		c.Close()
		return nil, 0, fmt.Errorf("choose a regular file of 1–%d bytes", limit)
	}
	return &smbFile{File: file, connection: c}, info.Size(), nil
}
