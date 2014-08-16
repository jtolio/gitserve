// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"bytes"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"code.google.com/p/go.crypto/ssh"
	gs_ssh "github.com/jtolds/gitserve/ssh"
)

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
		// TODO: huh, os/exec doesn't actually let me see the exit status?
		//  exec.ExitError/os.ProcessState seems like they should, but
		//  cross-platform compatibility i guess?
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
