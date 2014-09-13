// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"code.google.com/p/go.crypto/ssh"
	gs_ssh "github.com/jtolds/gitserve/ssh"
	"github.com/spacemonkeygo/monotime"
)

type SubmissionHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string, tags []string) (
	exit_status uint32,
	err error)

type AuthHandler func(meta ssh.ConnMetadata, key ssh.PublicKey) error

type NewRepoHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string) error

type RepoSubmissions struct {
	PrivateKey        ssh.Signer
	ShellError        string
	MOTD              string
	StoragePath       string
	Clean             bool
	SubmissionHandler SubmissionHandler
	AuthHandler       AuthHandler
	NewRepoHandler    NewRepoHandler
	MaxPushSize       int64

	mtx          sync.Mutex
	repo_lock_cv *sync.Cond
	keys         map[string]ssh.PublicKey
	repo_locks   map[string]bool
}

func (rs *RepoSubmissions) getKey(session_id []byte) ssh.PublicKey {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.keys == nil {
		return nil
	}
	return rs.keys[string(session_id)]
}

func (rs *RepoSubmissions) lockRepo(repo_id string) {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.repo_lock_cv == nil {
		rs.repo_lock_cv = sync.NewCond(&rs.mtx)
	}
	if rs.repo_locks == nil {
		rs.repo_locks = make(map[string]bool)
	}
	for rs.repo_locks[repo_id] {
		rs.repo_lock_cv.Wait()
	}
	rs.repo_locks[repo_id] = true
}

func (rs *RepoSubmissions) unlockRepo(repo_id string) {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	delete(rs.repo_locks, repo_id)
	rs.repo_lock_cv.Broadcast()
}

func (rs *RepoSubmissions) repoId(key ssh.PublicKey, repo_name string) string {
	full_keyhash := sha256.Sum256([]byte(string(ssh.MarshalAuthorizedKey(key)) +
		" " + repo_name))
	return hex.EncodeToString(full_keyhash[:16])
}

func (rs *RepoSubmissions) getUserRepo(repo_id string, output io.Writer,
	meta ssh.ConnMetadata, key ssh.PublicKey, repo_name string) (
	path string, err error) {
	user_repo := filepath.Join(rs.StoragePath, repo_id)
	_, err = os.Stat(user_repo)
	if err == nil {
		return user_repo, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	err = os.MkdirAll(user_repo, 0755)
	if err != nil {
		return "", err
	}

	if rs.NewRepoHandler != nil {
		err = rs.NewRepoHandler(user_repo, output, meta, key, repo_name)
		if err != nil {
			os.RemoveAll(user_repo)
			return "", err
		}
	} else {
		err = exec.Command(
			"git", "--git-dir", user_repo, "init", "--bare").Run()
		if err != nil {
			os.RemoveAll(user_repo)
			return "", err
		}
	}

	return user_repo, nil
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
	if len(parts) != 2 || (parts[0] != "git-receive-pack" &&
		parts[0] != "git-upload-pack") {
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}

	repo_name := strings.Trim(parts[1], "'")

	repo_id := rs.repoId(key, repo_name)
	rs.lockRepo(repo_id)
	user_repo, err := rs.getUserRepo(repo_id, stderr, meta, key, repo_name)
	if err != nil {
		rs.unlockRepo(repo_id)
		return 1, err
	}
	if rs.Clean {
		defer func() {
			os.RemoveAll(user_repo)
			rs.unlockRepo(repo_id)
		}()
	} else {
		rs.unlockRepo(repo_id)
	}

	if parts[0] != "git-receive-pack" {
		logger.Infof("git fetch: %s %s %s", meta.User(), repo_name, user_repo)
		start_time := monotime.Monotonic()
		cmd := exec.Command("git-upload-pack", user_repo)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		exit_status, err = RunExec(cmd)
		logger.Noticef("git fetch: %s %s %s [took %s]", meta.User(), repo_name,
			user_repo, monotime.Monotonic()-start_time)
		return exit_status, err
	}

	logger.Infof("git push: %s %s %s", meta.User(), repo_name, user_repo)
	start_time := monotime.Monotonic()
	cmd := exec.Command("git-receive-pack", user_repo)
	tags := &tagger{Reader: &maxReader{Reader: stdin, Max: rs.MaxPushSize}}
	cmd.Stdin = tags
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	exit_status, err = RunExec(cmd)
	logger.Noticef("git push: %s %s %s [took %s]", meta.User(), repo_name,
		user_repo, monotime.Monotonic()-start_time)
	if err != nil {
		if tags.Err != nil {
			fmt.Fprintf(stderr, "error: %s\n", tags.Err)
		}
		return exit_status, err
	}

	if rs.SubmissionHandler != nil {
		start_time := monotime.Monotonic()
		exit_status, err = rs.SubmissionHandler(user_repo, stderr, meta, key,
			repo_name, tags.NewTags)
		logger.Infof("processed submission: %s %s %s [took %s]", meta.User(), repo_name, user_repo, monotime.Monotonic()-start_time)
		return exit_status, err
	}
	return 0, nil
}

func (rs *RepoSubmissions) publicKeyCallback(
	meta ssh.ConnMetadata, key ssh.PublicKey) (rv *ssh.Permissions, err error) {
	defer mon.Task()(&err)

	if rs.AuthHandler != nil {
		err = rs.AuthHandler(meta, key)
		if err != nil {
			return nil, err
		}
	}

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
