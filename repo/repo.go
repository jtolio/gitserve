// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"code.google.com/p/go.crypto/ssh"
	gs_ssh "github.com/jtolds/gitserve/ssh"
	"github.com/spacemonkeygo/monitor"
	"github.com/spacemonkeygo/spacelog"
)

var (
	logger = spacelog.GetLogger()
	mon    = monitor.GetMonitors()
)

type SubmissionHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string) (
	exit_status uint32,
	err error)

type RepoSubmissions struct {
	PrivateKey  ssh.Signer
	ShellError  string
	MOTD        string
	StoragePath string
	Keep        bool
	Handler     SubmissionHandler
	MaxRepoSize int64

	mtx  sync.Mutex
	keys map[string]ssh.PublicKey
}

func (rs *RepoSubmissions) getKey(session_id []byte) ssh.PublicKey {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.keys == nil {
		return nil
	}
	return rs.keys[string(session_id)]
}

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

func (rs *RepoSubmissions) cmdHandler(command string,
	stdin io.Reader, stdout, stderr io.Writer,
	meta ssh.ConnMetadata) (exit_status uint32, err error) {
	defer mon.Task()(&err)
	key := rs.getKey(meta.SessionID())
	if key == nil {
		panic("unauthorized?")
	}
	parts := strings.Split(command, " ")
	if len(parts) != 2 || parts[0] != "git-receive-pack" {
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}

	tmpdir, err := ioutil.TempDir(rs.StoragePath, "submission-")
	if err != nil {
		return 1, err
	}
	if !rs.Keep {
		defer os.RemoveAll(tmpdir)
	}

	err = exec.Command("git", "--git-dir", tmpdir, "init", "--bare").Run()
	if err != nil {
		return 1, err
	}

	cmd := exec.Command("git-receive-pack", tmpdir)
	cmd.Stdin = &maxReader{Reader: stdin, Max: rs.MaxRepoSize}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if err != nil {
		// TODO: huh, os/exec doesn't actually let me see the exit status
		return 1, err
	}

	if rs.Handler != nil {
		return rs.Handler(tmpdir, stderr, meta, key, strings.Trim(parts[1], "'"))
	}
	return 0, nil
}

func (rs *RepoSubmissions) publicKeyCallback(
	meta ssh.ConnMetadata, key ssh.PublicKey) (rv *ssh.Permissions, err error) {
	defer mon.Task()(&err)
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.keys == nil {
		rs.keys = make(map[string]ssh.PublicKey)
	}

	session_id := string(meta.SessionID())
	if _, exists := rs.keys[session_id]; exists {
		panic("session should be unique")
	}
	rs.keys[session_id] = key
	return nil, nil
}

func (rs *RepoSubmissions) sessionEnd(meta ssh.ConnMetadata) {
	defer mon.Task()(nil)
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.keys != nil {
		delete(rs.keys, string(meta.SessionID()))
	}
}

func (rs *RepoSubmissions) ListenAndServe(network, address string) (err error) {
	defer mon.Task()(&err)
	config := &ssh.ServerConfig{PublicKeyCallback: rs.publicKeyCallback}
	config.AddHostKey(rs.PrivateKey)
	return (&gs_ssh.RestrictedServer{
		SSHConfig:  config,
		ShellError: rs.ShellError,
		MOTD:       rs.MOTD,
		Handler:    rs.cmdHandler,
		SessionEnd: rs.sessionEnd}).ListenAndServe(network, address)
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

type RepoHosting struct {
	PrivateKey     ssh.Signer
	ShellError     string
	MOTD           string
	RepoBase       string
	AuthorizedKeys []ssh.PublicKey
}

func (rh *RepoHosting) cmdHandler(command string,
	stdin io.Reader, stdout, stderr io.Writer,
	meta ssh.ConnMetadata) (exit_status uint32, err error) {
	defer mon.Task()(&err)
	parts := strings.Split(command, " ")
	if len(parts) != 2 || (parts[0] != "git-receive-pack" &&
		parts[0] != "git-upload-pack") {
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}
	repo := strings.Trim(parts[1], "'/")
	if strings.Contains(repo, "/") {
		_, err = fmt.Fprintf(stderr, "invalid repo: %#v\r\n", repo)
		return 1, err
	}
	cmd := exec.Command(parts[0], filepath.Join(rh.RepoBase, repo))
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err = cmd.Run()
	if err != nil {
		// TODO: huh, os/exec doesn't actually let me see the exit status
		return 1, err
	}
	return 0, nil
}

func (rh *RepoHosting) publicKeyCallback(
	meta ssh.ConnMetadata, key ssh.PublicKey) (rv *ssh.Permissions, err error) {
	defer mon.Task()(&err)
	for _, auth_key := range rh.AuthorizedKeys {
		// TODO: i'm not sure if this is the right way to compare key equality,
		//  but this is at least as strict as doing it the right way.
		if bytes.Equal(ssh.MarshalAuthorizedKey(auth_key),
			ssh.MarshalAuthorizedKey(key)) {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("invalid user")
}

func (rh *RepoHosting) ListenAndServe(network, address string) (err error) {
	defer mon.Task()(&err)
	config := &ssh.ServerConfig{PublicKeyCallback: rh.publicKeyCallback}
	config.AddHostKey(rh.PrivateKey)
	return (&gs_ssh.RestrictedServer{
		SSHConfig:  config,
		ShellError: rh.ShellError,
		MOTD:       rh.MOTD,
		Handler:    rh.cmdHandler}).ListenAndServe(network, address)
}
