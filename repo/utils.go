// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"bytes"
	"fmt"
	"io"

	"code.google.com/p/go.crypto/ssh"
	"github.com/spacemonkeygo/monitor"
	"github.com/spacemonkeygo/spacelog"
)

var (
	logger = spacelog.GetLogger()
	mon    = monitor.GetMonitors()
)

type maxReader struct {
	Reader io.Reader
	Pos    int64
	Max    int64
}

func (m *maxReader) Read(p []byte) (n int, err error) {
	n, err = m.Reader.Read(p)
	m.Pos += int64(n)
	if m.Pos > m.Max {
		return 0, fmt.Errorf("data exceeded limit %d", m.Max)
	}
	return n, err
}

func LoadAuthorizedKeys(data []byte) (rv []ssh.PublicKey, err error) {
	data = bytes.TrimSpace(data)
	for len(data) > 0 {
		key, _, _, rest, err := ssh.ParseAuthorizedKey(data)
		if err != nil {
			return rv, err
		}
		data = bytes.TrimSpace(rest)
		rv = append(rv, key)
	}
	return rv, nil
}
