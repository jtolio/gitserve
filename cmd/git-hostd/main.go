// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package main

import (
	"flag"
	"io/ioutil"
	"net/http"

	"github.com/jtolds/gitserve/repo"
	"github.com/spacemonkeygo/flagfile"
	"github.com/spacemonkeygo/spacelog"
	"github.com/spacemonkeygo/spacelog/setup"
	"golang.org/x/crypto/ssh"
	"gopkg.in/spacemonkeygo/monitor.v1"
)

var (
	addr       = flag.String("addr", ":7022", "address to listen on for ssh")
	privateKey = flag.String("private_key", "",
		"path to server private key. If not provided, one will be generated")
	shellError = flag.String("shell_error",
		"Sorry, no interactive shell available.",
		"the message to display to interactive users")
	motd = flag.String("motd",
		"Welcome to the gitserve git-hostd code hosting tool!\r\n"+
			"Please see https://github.com/jtolds/gitserve for more info.\r\n",
		"the motd banner")
	repoBase = flag.String("repo_base", "",
		"If set, the folder to serve git repos out of. Ignored if --repo is set.")
	repoPath = flag.String("repo", "",
		"If set, the repo to serve. Overrides --repo_base. If neither "+
			"--repo_base or --repo are set, the current directory is used")
	authorizedKeys = flag.String("authorized_keys", "",
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

	rh := &repo.RepoHosting{
		ShellError: *shellError + "\r\n",
		MOTD:       *motd + "\r\n",
		RepoBase:   *repoBase,
		Repo:       *repoPath}

	if *privateKey != "" {
		logger.Noticef("Using %#v as server's private key", *privateKey)
		private_bytes, err := ioutil.ReadFile(*privateKey)
		if err != nil {
			panic(err)
		}
		rh.PrivateKey, err = ssh.ParsePrivateKey(private_bytes)
		if err != nil {
			panic(err)
		}
	}

	if *authorizedKeys != "" {
		authorized_bytes, err := ioutil.ReadFile(*authorizedKeys)
		if err != nil {
			panic(err)
		}
		rh.AuthorizedKeys, err = repo.LoadAuthorizedKeys(authorized_bytes)
		if err != nil {
			panic(err)
		}
	}

	panic(rh.ListenAndServe("tcp", *addr))
}
