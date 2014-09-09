// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package main

import (
	"flag"
	"io"
	"io/ioutil"
	"net/http"
	"os/exec"
	"strings"

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
		"Welcome to the gitserve git-submitd code repo submission tool!\r\n"+
			"Please see https://github.com/jtolds/gitserve for more info.\r\n",
		"the motd banner")
	storage = flag.String("storage_path", "/tmp",
		"storage path for git submissions")
	clean = flag.Bool("clean", false,
		"if true, deletes repos after processing, instead of keeping")
	inspect = flag.String("inspect", "./submission-trigger.py",
		"the subprocess to run on a git repo submission")
	auth = flag.String("auth", "",
		"If set, will be run with incoming SSH keys prior to receiving packs. "+
			"A successful exit status will let a receive go through")
	newRepo = flag.String("new_repo", "",
		"If set, will be run to initiate a new repo. the --repo argument given "+
			"will be an empty folder that should be a bare git repo when this "+
			"command is done.")
	debugAddr = flag.String("debug_addr", "127.0.0.1:0",
		"address to listen on for debug http endpoints")
	maxPushSize = flag.Uint64("max_push_size", 256*1024*1024,
		"the maximum push size in bytes")

	logger = spacelog.GetLogger()
	mon    = monitor.GetMonitors()
)

func SubmissionHandler(repo_path string, output io.Writer,
	meta ssh.ConnMetadata, key ssh.PublicKey, name string, tags []string) (
	exit_status uint32, err error) {
	defer mon.Task()(&err)
	cmd := exec.Command(*inspect,
		"--repo", repo_path,
		"--user", meta.User(),
		"--remote", meta.RemoteAddr().String(),
		"--key", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
		"--name", name,
		"--tags", strings.Join(tags, "\x00"))
	cmd.Stdout = output
	cmd.Stderr = output
	return repo.RunExec(cmd)
}

func NewRepoHandler(repo_path string, output io.Writer, meta ssh.ConnMetadata,
	key ssh.PublicKey, name string) (err error) {
	defer mon.Task()(&err)
	cmd := exec.Command(*newRepo,
		"--repo", repo_path,
		"--user", meta.User(),
		"--remote", meta.RemoteAddr().String(),
		"--key", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))),
		"--name", name)
	cmd.Stdout = output
	cmd.Stderr = output
	return cmd.Run()
}

func AuthHandler(meta ssh.ConnMetadata, key ssh.PublicKey) (err error) {
	defer mon.Task()(&err)
	if *auth == "" {
		return nil
	}
	return exec.Command(*auth,
		"--user", meta.User(),
		"--remote", meta.RemoteAddr().String(),
		"--key", strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key)))).Run()
}

func main() {
	flagfile.Load()
	setup.MustSetup("git-submitd")
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

	var new_repo repo.NewRepoHandler
	if *newRepo != "" {
		new_repo = NewRepoHandler
	}

	panic((&repo.RepoSubmissions{
		PrivateKey:        private_key,
		ShellError:        *shellError + "\r\n",
		MOTD:              *motd + "\r\n",
		StoragePath:       *storage,
		Clean:             *clean,
		SubmissionHandler: SubmissionHandler,
		AuthHandler:       AuthHandler,
		NewRepoHandler:    new_repo,
		MaxPushSize:       int64(*maxPushSize)}).ListenAndServe("tcp", *addr))
}
