// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	gs_ssh "github.com/jtolds/gitserve/ssh"
	"golang.org/x/crypto/ssh"
)

type RepoHosting struct {
	ShellError string
	MOTD       string

	// if unset, one will be generated
	PrivateKey ssh.Signer

	// path to the directory containing repos to serve.
	RepoBase string
	// if set, overrides RepoBase + user-supplied repo name. if neither
	// RepoBase or Repo are set, Repo defaults to "."
	Repo string

	// if empty, *all* users will be allowed.
	AuthorizedKeys []ssh.PublicKey

	// If set, these commands override the default git-receive-pack and
	// git-upload-pack
	GitReceivePack string
	GitUploadPack  string
}

func (rh *RepoHosting) cmdHandler(command string,
	stdin io.Reader, stdout, stderr io.Writer,
	meta ssh.ConnMetadata) (exit_status uint32, err error) {
	defer mon.Task()(&err)

	parts := strings.Split(command, " ")
	if len(parts) != 2 {
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}

	os_cmd := parts[0]
	switch os_cmd {
	case "git-receive-pack":
		if rh.GitReceivePack != "" {
			os_cmd = rh.GitReceivePack
		}
	case "git-upload-pack":
		if rh.GitUploadPack != "" {
			os_cmd = rh.GitUploadPack
		}
	default:
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}

	repo := strings.Trim(parts[1], "'/")
	if strings.Contains(repo, "/") {
		_, err = fmt.Fprintf(stderr, "invalid repo: %#v\r\n", repo)
		return 1, err
	}

	var repo_path string
	if rh.Repo != "" {
		repo_path = rh.Repo
	} else if rh.RepoBase != "" {
		repo_path = filepath.Join(rh.RepoBase, repo)
	} else {
		repo_path = "."
	}

	logger.Noticef("Remote request for repo %#v", repo_path)
	cmd := exec.Command(os_cmd, repo_path)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return RunExec(cmd)
}

func (rh *RepoHosting) publicKeyCallback(
	meta ssh.ConnMetadata, key ssh.PublicKey) (rv *ssh.Permissions, err error) {
	defer mon.Task()(&err)

	if len(rh.AuthorizedKeys) == 0 {
		logger.Noticef("All users authorized")
		return nil, nil
	}

	for _, auth_key := range rh.AuthorizedKeys {
		// TODO: i'm not sure if this is the right way to compare key equality,
		//  but this is at least as strict as doing it the right way.
		if bytes.Equal(ssh.MarshalAuthorizedKey(auth_key),
			ssh.MarshalAuthorizedKey(key)) {
			logger.Infof("User authorized")
			return nil, nil
		}
	}

	logger.Warnf("User not in authorized keys file, rejecting")
	return nil, fmt.Errorf("invalid user")
}

func (rh *RepoHosting) ListenAndServe(network, address string) (err error) {
	defer mon.Task()(&err)
	config := &ssh.ServerConfig{PublicKeyCallback: rh.publicKeyCallback}

	if rh.PrivateKey == nil {
		logger.Noticef("No private key specified, generating a new one")
		rsa_key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return err
		}
		rh.PrivateKey, err = ssh.NewSignerFromKey(rsa_key)
		if err != nil {
			return err
		}
	}
	config.AddHostKey(rh.PrivateKey)

	return (&gs_ssh.RestrictedServer{
		SSHConfig:  config,
		ShellError: rh.ShellError,
		MOTD:       rh.MOTD,
		Handler:    rh.cmdHandler}).ListenAndServe(network, address)
}
