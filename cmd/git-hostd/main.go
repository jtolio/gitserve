// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package main

import (
	"flag"
	"io/ioutil"
	"net/http"

	"code.google.com/p/go.crypto/ssh"
	"github.com/jtolds/gitserve/repo"
	"github.com/spacemonkeygo/flagfile"
	"github.com/spacemonkeygo/monitor"
	"github.com/spacemonkeygo/spacelog"
	"github.com/spacemonkeygo/spacelog/setup"
)

var (
	addr       = flag.String("addr", ":0", "address to listen on for ssh")
	privateKey = flag.String("private_key", "id_rsa",
		"path to server private key")
	shellError = flag.String("shell_error",
		"Sorry, no interactive shell available.",
		"the message to display to interactive users")
	motd = flag.String("motd",
		"Welcome to the gitserve git-hostd code hosting tool!\r\n"+
			"Please see https://github.com/jtolds/gitserve for more info.\r\n",
		"the motd banner")
	repoBase = flag.String("repo_base", "",
		"If set, the folder to serve git repos out of. Ignored if --repo is set")
	repoPath = flag.String("repo", "",
		"If set, the repo to serve. Overrides --repo_base")
	authorizedKeys = flag.String("authorized_keys", "authorized_keys",
		"the authorized key file")
	debugAddr = flag.String("debug_addr", "127.0.0.1:0",
		"address to listen on for debug http endpoints")

	logger = spacelog.GetLogger()
	mon    = monitor.GetMonitors()
)

func main() {
	flagfile.Load()
	setup.MustSetup("git-hostd")
	monitor.RegisterEnvironment()
	go http.ListenAndServe(*debugAddr, monitor.DefaultStore)

	private_bytes, err := ioutil.ReadFile(*privateKey)
	if err != nil {
		panic(err)
	}
	private_key, err := ssh.ParsePrivateKey(private_bytes)
	if err != nil {
		panic(err)
	}

	authorized_bytes, err := ioutil.ReadFile(*authorizedKeys)
	if err != nil {
		panic(err)
	}
	auth_keys, err := repo.LoadAuthorizedKeys(authorized_bytes)
	if err != nil {
		panic(err)
	}

	repo_path := *repoPath
	if repo_path == "" && *repoBase == "" {
		repo_path = "."
	}

	panic((&repo.RepoHosting{
		PrivateKey:     private_key,
		ShellError:     *shellError + "\r\n",
		MOTD:           *motd + "\r\n",
		RepoBase:       *repoBase,
		Repo:           repo_path,
		AuthorizedKeys: auth_keys}).ListenAndServe("tcp", *addr))
}
