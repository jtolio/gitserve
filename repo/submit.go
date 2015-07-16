// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	gs_ssh "github.com/jtolds/gitserve/ssh"
	"github.com/spacemonkeygo/monotime"
	"golang.org/x/crypto/ssh"
)

type SubmissionHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string, tags map[Ref][]Tag) (
	exit_status uint32,
	err error)

type PresubmissionHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string) (
	err error)

type AuthHandler func(meta ssh.ConnMetadata, key ssh.PublicKey) (
	unique_user_id *string, err error)

type NewRepoHandler func(
	repo_path string,
	output io.Writer,
	meta ssh.ConnMetadata,
	key ssh.PublicKey,
	repo_name string) error

type session struct {
	key            ssh.PublicKey
	unique_user_id string
}

type RepoSubmissions struct {
	PrivateKey           ssh.Signer
	ShellError           string
	MOTD                 string
	StoragePath          func(user_id, repo_name string) string
	Clean                bool
	PresubmissionHandler PresubmissionHandler
	SubmissionHandler    SubmissionHandler
	AuthHandler          AuthHandler
	NewRepoHandler       NewRepoHandler
	MaxPushSize          int64

	// If set, these commands override the default git-receive-pack and
	// git-upload-pack
	GitReceivePack string
	GitUploadPack  string

	mtx          sync.Mutex
	repo_lock_cv *sync.Cond
	sessions     map[string]*session
	repo_locks   map[string]bool
}

func (rs *RepoSubmissions) getSession(session_id []byte) *session {
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.sessions == nil {
		return nil
	}
	return rs.sessions[string(session_id)]
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

func userIdFromKey(key ssh.PublicKey) string {
	keyhash := sha256.Sum256(ssh.MarshalAuthorizedKey(key))
	return hex.EncodeToString(keyhash[:])
}

func (rs *RepoSubmissions) repoPath(unique_user_id, repo_name string) string {
	if rs.StoragePath != nil {
		return rs.StoragePath(unique_user_id, repo_name)
	}
	mac := hmac.New(sha256.New, []byte(unique_user_id))
	mac.Write([]byte(repo_name))
	id := mac.Sum(nil)
	return fmt.Sprintf("/tmp/submissions/%x", id)
}

func (rs *RepoSubmissions) getUserRepo(user_repo string, output io.Writer,
	meta ssh.ConnMetadata, key ssh.PublicKey, repo_name string) (
	path string, err error) {
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
	session := rs.getSession(meta.SessionID())
	if session == nil {
		panic("unauthorized?")
	}
	parts := strings.Split(command, " ")
	if len(parts) != 2 || (parts[0] != "git-receive-pack" &&
		parts[0] != "git-upload-pack") {
		_, err = fmt.Fprintf(stderr, "invalid command: %#v\r\n", command)
		return 1, err
	}

	repo_name := strings.Trim(parts[1], "'")

	repo_path := rs.repoPath(session.unique_user_id, repo_name)
	rs.lockRepo(repo_path)
	user_repo, err := rs.getUserRepo(repo_path, stderr, meta, session.key,
		repo_name)
	if err != nil {
		rs.unlockRepo(repo_path)
		return 1, err
	}
	if rs.Clean {
		defer func() {
			os.RemoveAll(user_repo)
			rs.unlockRepo(repo_path)
		}()
	} else {
		rs.unlockRepo(repo_path)
	}

	if parts[0] != "git-receive-pack" {
		logger.Infof("git fetch: %s %s %s", meta.User(), repo_name, user_repo)
		os_cmd := "git-upload-pack"
		if rs.GitUploadPack != "" {
			os_cmd = rs.GitUploadPack
		}
		start_time := monotime.Monotonic()
		cmd := exec.Command(os_cmd, user_repo)
		cmd.Stdin = stdin
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		exit_status, err = RunExec(cmd)
		logger.Noticef("git fetch: %s %s %s [took %s]", meta.User(), repo_name,
			user_repo, monotime.Monotonic()-start_time)
		return exit_status, err
	}

	if rs.PresubmissionHandler != nil {
		err := rs.PresubmissionHandler(user_repo, stderr, meta, session.key,
			repo_name)
		if err != nil {
			return 1, err
		}
	}

	logger.Infof("git push: %s %s %s", meta.User(), repo_name, user_repo)
	os_cmd := "git-receive-pack"
	if rs.GitReceivePack != "" {
		os_cmd = rs.GitReceivePack
	}
	start_time := monotime.Monotonic()
	cmd := exec.Command(os_cmd, user_repo)
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
		exit_status, err = rs.SubmissionHandler(user_repo, stderr, meta,
			session.key, repo_name, tags.NewTags)
		logger.Infof("processed submission: %s %s %s [took %s]", meta.User(),
			repo_name, user_repo, monotime.Monotonic()-start_time)
		return exit_status, err
	}
	return 0, nil
}

func (rs *RepoSubmissions) publicKeyCallback(
	meta ssh.ConnMetadata, key ssh.PublicKey) (rv *ssh.Permissions, err error) {
	defer mon.Task()(&err)

	var unique_user_id *string
	if rs.AuthHandler != nil {
		unique_user_id, err = rs.AuthHandler(meta, key)
		if err != nil {
			return nil, err
		}
	}
	if unique_user_id == nil {
		id := userIdFromKey(key)
		unique_user_id = &id
	}

	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.sessions == nil {
		rs.sessions = make(map[string]*session)
	}

	session_id := string(meta.SessionID())
	if _, exists := rs.sessions[session_id]; exists {
		panic("session should be unique")
	}
	rs.sessions[session_id] = &session{key: key, unique_user_id: *unique_user_id}
	return nil, nil
}

func (rs *RepoSubmissions) sessionEnd(meta ssh.ConnMetadata) {
	defer mon.Task()(nil)
	rs.mtx.Lock()
	defer rs.mtx.Unlock()
	if rs.sessions != nil {
		delete(rs.sessions, string(meta.SessionID()))
	}
}

func (rs *RepoSubmissions) ListenAndServe(network, address string) (
	err error) {
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
