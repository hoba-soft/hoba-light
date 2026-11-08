package hobalight

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitCode(t *testing.T) {
	partial := &partialBackupError{errors.New("incomplete")}
	for _, tc := range []struct {
		err  error
		want int
	}{
		{nil, 0}, {errors.New("failed"), 1}, {partial, 2},
		{fmt.Errorf("source: %w", partial), 2},
		{errors.Join(partial, partial), 2},
		{errors.Join(partial, errors.New("failed")), 1},
		{errors.Join(errors.New("failed"), partial), 1},
	} {
		if got := ExitCode(tc.err); got != tc.want {
			t.Fatalf("%v: got %d, want %d", tc.err, got, tc.want)
		}
	}
}

func TestDestructiveCommandsRejectAmbiguousPrefix(t *testing.T) {
	s := newStore(t, t.TempDir())
	file := strings.Repeat("a", 63) + "fF"
	session := strings.Repeat("a", 63) + "eS"
	writeFile(t, s.object(file), []byte("file"))
	writeFile(t, s.object(session), []byte("session"))
	for _, run := range []func() error{
		func() error {
			return s.deleteCommand([]string{"--force", "aaaaaaaa"}, strings.NewReader(""), io.Discard)
		},
		func() error { return s.restoreCommand([]string{"aaaaaaaa"}, false, io.Discard) },
		func() error { return s.restoreCommand([]string{"aaaaaaaa", ".", t.TempDir()}, true, io.Discard) },
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "ambiguous hash") {
			t.Fatalf("got %v", err)
		}
		if string(readFile(t, s.object(file))) != "file" || string(readFile(t, s.object(session))) != "session" {
			t.Fatal("ambiguous command changed objects")
		}
	}
}

func TestForcedDeletionHasNoQuestions(t *testing.T) {
	for _, kind := range []string{"session", "file"} {
		t.Run(kind, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "file"), []byte("contents"))
			s := newStore(t, filepath.Join(root, "store"))
			id, err := s.backup(source, false)
			must(t, err)
			if kind == "file" {
				_, files, err := s.sessionFiles(id)
				must(t, err)
				id = files[0].id
			}
			var out bytes.Buffer
			must(t, s.deleteCommand([]string{"--force", id}, strings.NewReader(""), &out))
			if strings.Contains(out.String(), "?") || strings.Contains(out.String(), "[y/N]") {
				t.Fatal(out.String())
			}
		})
	}
}
