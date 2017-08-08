// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package ssh

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/spacemonkeygo/spacelog"
	"golang.org/x/crypto/ssh"
	"gopkg.in/spacemonkeygo/monkit.v2"
)

var (
	logger = spacelog.GetLogger()
	mon    = monkit.Package()
)

type CommandHandler func(
	command string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	meta ssh.ConnMetadata) (
	exit_status uint32, err error)

type RestrictedServer struct {
	SSHConfig  *ssh.ServerConfig
	ShellError string
	MOTD       string
	Handler    CommandHandler
	SessionEnd func(meta ssh.ConnMetadata)
}

func writeExitStatus(ch ssh.Channel, status uint32) (err error) {
	defer mon.Task()(nil)(&err)
	var packed [4]byte
	binary.BigEndian.PutUint32(packed[:], status)
	_, err = ch.SendRequest("exit-status", false, packed[:])
	return err
}

func (r *RestrictedServer) handleExec(ch ssh.Channel, command string,
	meta ssh.ConnMetadata) (exit_status uint32, err error) {
	defer mon.Task()(nil)(&err)
	if r.Handler != nil {
		return r.Handler(command, ch, ch, ch.Stderr(), meta)
	}
	_, err = ch.Stderr().Write([]byte(
		fmt.Sprintf("command rejected: %#v\r\n", command)))
	return 1, err
}

func (r *RestrictedServer) handleChan(ch ssh.Channel,
	reqs <-chan *ssh.Request, meta ssh.ConnMetadata) (err error) {
	defer mon.Task()(nil)(&err)
	defer ch.Close()
	exec_happened := false
	pty_requested := false
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			pty_requested = true
			err := req.Reply(true, nil)
			if err != nil {
				return err
			}
			continue
		case "shell":
			err := req.Reply(true, nil)
			if err != nil {
				return err
			}
			if r.MOTD != "" {
				_, err = ch.Stderr().Write([]byte(r.MOTD))
				if err != nil {
					return err
				}
			}
			if r.ShellError != "" {
				_, err = ch.Stderr().Write([]byte(r.ShellError))
				if err != nil {
					return err
				}
			}
			return writeExitStatus(ch, 1)
		case "env":
			err := req.Reply(false, nil)
			if err != nil {
				return err
			}
			continue
		}
		if req.Type != "exec" || exec_happened || pty_requested ||
			len(req.Payload) < 4 {
			err := req.Reply(false, nil)
			if err != nil {
				return err
			}
			continue
		}
		exec_happened = true

		payload_len := binary.BigEndian.Uint32(req.Payload[:4])
		if len(req.Payload)-4 != int(payload_len) {
			// it's unclear to me what's going on here - i so far haven't found
			// documentation (though i haven't looked very hard) about what a request
			// payload is supposed to be and why it seems like the network endian
			// size of it is a prefix, even though the payload already has a length
			// in go-land. unsure if this is a library-specific issue or part of the
			// ssh protocol expectations. anyway, be paranoid. kill everything if
			// my assumption is wrong.
			panic(fmt.Sprintf("whups, bad assumption, %d != %d",
				len(req.Payload)-4, payload_len))
		}

		err := req.Reply(true, nil)
		if err != nil {
			return err
		}

		go func() {
			defer ch.Close()
			if r.MOTD != "" {
				_, err = ch.Stderr().Write([]byte(r.MOTD))
				if err != nil {
					logger.Errore(err)
					logger.Errore(writeExitStatus(ch, 1))
					return
				}
			}

			exit_status, err := r.handleExec(ch, string(req.Payload[4:]), meta)
			if err != nil {
				logger.Errore(err)
				logger.Errore(writeExitStatus(ch, 1))
			} else {
				logger.Errore(writeExitStatus(ch, exit_status))
			}
		}()
	}
	return nil
}

func (r *RestrictedServer) handleConn(conn net.Conn) (err error) {
	defer mon.Task()(nil)(&err)
	defer conn.Close()
	sc, new_chans, reqs, err := ssh.NewServerConn(conn, r.SSHConfig)
	if err != nil {
		return err
	}
	defer sc.Close()
	if r.SessionEnd != nil {
		defer r.SessionEnd(sc.Conn)
	}
	go ssh.DiscardRequests(reqs)

	for new_chan := range new_chans {
		if new_chan.ChannelType() != "session" {
			new_chan.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		ch, reqs, err := new_chan.Accept()
		if err != nil {
			return fmt.Errorf("could not accept channel")
		}
		go func() {
			logger.Errore(r.handleChan(ch, reqs, sc.Conn))
		}()
	}
	return nil
}

func (r *RestrictedServer) Serve(listener net.Listener) (err error) {
	defer mon.Task()(nil)(&err)
	defer listener.Close()
	logger.Noticef("listening on %s", listener.Addr())
	var delay time.Duration
	for {
		conn, err := listener.Accept()
		if err != nil {
			if net_err, ok := err.(net.Error); ok && net_err.Temporary() {
				if delay == 0 {
					delay = 5 * time.Millisecond
				} else {
					delay *= 2
				}
				if max := 1 * time.Second; delay > max {
					delay = max
				}
				logger.Errorf("http: Accept error: %v; retrying in %v", err, delay)
				time.Sleep(delay)
				continue
			}
			return err
		}
		delay = 0
		go func() {
			err := r.handleConn(conn)
			if err != nil && err != io.EOF {
				logger.Errore(err)
			}
		}()
	}
}

func (r *RestrictedServer) ListenAndServe(network, address string) (err error) {
	defer mon.Task()(nil)(&err)
	listener, err := net.Listen(network, address)
	if err != nil {
		return err
	}
	return r.Serve(listener)
}
