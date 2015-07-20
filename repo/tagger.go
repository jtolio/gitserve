// Copyright (C) 2014 JT Olds
// See LICENSE for copying information

package repo

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

type Ref string
type Tag string

type tagger struct {
	Reader       io.Reader
	pass_through bool
	SubmissionId string
	NewTags      map[Ref][]Tag
	Err          error
}

func (t *tagger) Read(p []byte) (n int, err error) {
	if t.Err != nil {
		return 0, t.Err
	}
	if t.pass_through {
		return t.Reader.Read(p)
	}
	if t.SubmissionId == "" {
		t.SubmissionId = fmt.Sprint(time.Now().UnixNano())
	}
	if t.NewTags == nil {
		t.NewTags = make(map[Ref][]Tag)
	}
	var buf bytes.Buffer
	for {
		var size_packed [4]byte
		_, err = io.ReadFull(t.Reader, size_packed[:])
		if err != nil {
			return 0, err
		}
		size, err := strconv.ParseUint(string(size_packed[:]), 16, 16)
		if err != nil {
			return 0, err
		}
		if size == 0 {
			break
		}
		_, err = buf.Write(size_packed[:])
		if err != nil {
			return 0, err
		}
		line := make([]byte, size-4)
		_, err = io.ReadFull(t.Reader, line)
		if err != nil {
			return 0, err
		}
		_, err = buf.Write(line)
		if err != nil {
			return 0, err
		}

		var fields []string
		parseable_part := string(line)
		null_index := strings.Index(parseable_part, "\x00")
		if null_index >= 0 {
			fields = strings.Fields(
				parseable_part[:null_index])
		}
		if len(fields) != 3 {
			t.Err = fmt.Errorf(
				"protocol error: unexpected amount of fields in pkt-line: %#v",
				parseable_part)
			return 0, t.Err
		}

		if strings.HasPrefix(fields[2], "refs/tags/submissions/") {
			t.Err = fmt.Errorf("pushing submission tags disallowed")
			return 0, t.Err
		}

		new_ref := fmt.Sprintf(
			"0000000000000000000000000000000000000000 %s "+
				"refs/tags/submissions/%s/%s\n", fields[1], t.SubmissionId, fields[2])

		new_size := fmt.Sprintf("%04x", len(new_ref)+4)
		if len(new_size) != 4 {
			t.Err = fmt.Errorf("tag name too long")
			return 0, t.Err
		}

		_, err = buf.Write([]byte(new_size))
		if err != nil {
			return 0, err
		}

		_, err = buf.Write([]byte(new_ref))
		if err != nil {
			return 0, err
		}

		t.NewTags[Ref(fields[1])] = append(t.NewTags[Ref(fields[1])],
			Tag(fmt.Sprintf("submissions/%s/%s", t.SubmissionId, fields[2])))
	}

	_, err = buf.Write([]byte("0000"))
	if err != nil {
		return 0, err
	}

	t.Reader = io.MultiReader(&buf, t.Reader)
	t.pass_through = true
	return t.Reader.Read(p)
}
