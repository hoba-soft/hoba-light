package hobalight

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestGCUsesSessionObjectsAsRoots(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeFile(t, filepath.Join(source, "file"), []byte("shared data"))
	s := newStore(t, filepath.Join(root, "store"))
	sessionID, err := s.backup(source, false)
	must(t, err)
	_, files, err := s.sessionFiles(sessionID)
	must(t, err)
	orphan := File{Identity: Identity{Path: "/unused", Size: 11}, Blob: files[0].metadata.Blob}
	data, err := encode(orphan)
	must(t, err)
	orphanID := hash(data) + "F"
	must(t, s.put(orphanID, data))
	blobData := []byte("unused blob bytes")
	blobID := hash(blobData) + "B"
	writeFile(t, s.object(blobID), blobData)
	chainFile := File{Identity: Identity{Path: "/also-unused", Size: int64(len(blobData))}, Blob: blobID}
	chainData, err := encode(chainFile)
	must(t, err)
	chainID := hash(chainData) + "F"
	must(t, s.put(chainID, chainData))
	// Even a reference-free session is a GC root. Reference YAML is never trusted.
	must(t, os.RemoveAll(filepath.Join(s.backend.LocalRoot(), "sessions")))
	var output bytes.Buffer
	must(t, s.gc(&output))
	if !strings.Contains(output.String(), "3 retained (") || !strings.Contains(output.String(), "3 moved to trash") {
		t.Fatal(output.String())
	}
	for _, id := range []string{sessionID, files[0].id, files[0].metadata.Blob} {
		if _, err := os.Stat(s.object(id)); err != nil {
			t.Fatal(err)
		}
	}
	for id, want := range map[string][]byte{orphanID: data, blobID: blobData, chainID: chainData} {
		if _, err := os.Stat(s.object(id)); !os.IsNotExist(err) {
			t.Fatal("orphan not moved")
		}
		matches, err := filepath.Glob(filepath.Join(s.backend.LocalRoot(), "trash", id+"-*.orphan"))
		must(t, err)
		if len(matches) != 1 || !bytes.Equal(readFile(t, matches[0]), want) {
			t.Fatal(matches)
		}
	}
	output.Reset()
	must(t, s.gc(&output))
	if !strings.Contains(output.String(), "0 moved to trash") {
		t.Fatal(output.String())
	}
}

func TestGCAbortsBeforeMovingOnDamagedGraph(t *testing.T) {
	for _, damage := range []string{"session", "file", "missing-file", "missing-blob"} {
		t.Run(damage, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeFile(t, filepath.Join(source, "file"), []byte("data"))
			s := newStore(t, filepath.Join(root, "store"))
			id, err := s.backup(source, false)
			must(t, err)
			_, files, err := s.sessionFiles(id)
			must(t, err)
			orphan := strings.Repeat("0", 64) + "B"
			writeFile(t, s.object(orphan), []byte("orphan"))
			switch damage {
			case "session":
				writeFile(t, s.object(id), []byte("broken"))
			case "file":
				writeFile(t, s.object(files[0].id), []byte("broken"))
			case "missing-file":
				must(t, os.Remove(s.object(files[0].id)))
			case "missing-blob":
				must(t, os.Remove(s.object(files[0].metadata.Blob)))
			}
			var out bytes.Buffer
			if err := s.gc(&out); err == nil || !strings.Contains(err.Error(), "before moving") {
				t.Fatalf("%v", err)
			}
			if _, err := os.Stat(s.object(orphan)); err != nil {
				t.Fatal("moved orphan before validating graph")
			}
		})
	}
}

func TestGCAndBackupCoordination(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	backup, err := s.beginBackup()
	must(t, err)
	if release, err := s.beginGC(); err == nil {
		release()
		t.Fatal("gc allowed during backup")
	} else {
		markers, globErr := filepath.Glob(filepath.Join(s.backend.LocalRoot(), ".backup-*"))
		must(t, globErr)
		if len(markers) != 1 || !strings.Contains(err.Error(), strconv.Quote(markers[0])) || !strings.Contains(err.Error(), "if no backup or refresh is running") || !strings.Contains(err.Error(), "remove this marker file and retry") {
			t.Fatalf("missing marker recovery guidance: %v", err)
		}
	}
	backup()
	gc, err := s.beginGC()
	must(t, err)
	if release, err := s.beginBackup(); err == nil {
		release()
		t.Fatal("backup allowed during gc")
	} else if !strings.Contains(err.Error(), strconv.Quote(filepath.Join(s.backend.LocalRoot(), ".gc.lock"))) || !strings.Contains(err.Error(), "if no maintenance operation is running") || !strings.Contains(err.Error(), "remove this lock directory and retry") {
		t.Fatalf("missing lock recovery guidance: %v", err)
	}
	if release, err := s.beginGC(); err == nil {
		release()
		t.Fatal("two collectors allowed")
	} else if !strings.Contains(err.Error(), strconv.Quote(filepath.Join(s.backend.LocalRoot(), ".gc.lock"))) || !strings.Contains(err.Error(), "remove this lock directory and retry") {
		t.Fatalf("missing lock recovery guidance: %v", err)
	}
	gc()
	release, err := s.beginBackup()
	must(t, err)
	release()
}

func TestGCWithNoSessionsAndTrashFailure(t *testing.T) {
	s := newStore(t, filepath.Join(t.TempDir(), "store"))
	id := hash([]byte("orphan")) + "B"
	writeFile(t, s.object(id), []byte("orphan"))
	writeFile(t, filepath.Join(s.backend.LocalRoot(), "trash"), []byte("obstruction"))
	var output bytes.Buffer
	if err := s.gc(&output); err == nil {
		t.Fatal("expected trash failure")
	}
	if _, err := os.Stat(s.object(id)); err != nil {
		t.Fatal("object lost")
	}
	must(t, os.Remove(filepath.Join(s.backend.LocalRoot(), "trash")))
	must(t, s.gc(&output))
	if !strings.Contains(output.String(), "0 retained (") || !strings.Contains(output.String(), "1 moved to trash") {
		t.Fatal(output.String())
	}
}

func TestGCRetainsOlderSessionsAndSharedBlobs(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	name := filepath.Join(source, "file")
	writeFile(t, name, []byte("first"))
	s := newStore(t, filepath.Join(root, "store"))
	first, err := s.backup(source, false)
	must(t, err)
	_, oldFiles, err := s.sessionFiles(first)
	must(t, err)
	writeFile(t, name, []byte("second, longer"))
	second, err := s.backup(source, false)
	must(t, err)
	_, newFiles, err := s.sessionFiles(second)
	must(t, err)
	var out bytes.Buffer
	must(t, s.gc(&out))
	if !strings.Contains(out.String(), "6 retained (") || !strings.Contains(out.String(), "0 moved") {
		t.Fatal(out.String())
	}
	for _, id := range []string{first, second, oldFiles[0].id, newFiles[0].id, oldFiles[0].metadata.Blob, newFiles[0].metadata.Blob} {
		if _, err := os.Stat(s.object(id)); err != nil {
			t.Fatal(err)
		}
	}
}
