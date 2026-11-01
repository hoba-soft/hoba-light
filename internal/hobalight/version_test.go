package hobalight

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestVersion(t *testing.T) {
	oldVersion, oldCommit := BuildVersion, BuildCommit
	defer func() { BuildVersion, BuildCommit = oldVersion, oldCommit }()
	BuildVersion, BuildCommit = "0.42.0", "123456789abcdef0"
	if Version() != "0.42.0-12345678" {
		t.Fatal(Version())
	}
}
func TestLegacySessionWithoutVersion(t *testing.T) {
	var s Session
	if err := yaml.Unmarshal([]byte("directory: /source\nstarted: yesterday\nfiles: []\n"), &s); err != nil {
		t.Fatal(err)
	}
	if s.Version != "" || s.Platform != "" || s.Directory != "/source" {
		t.Fatalf("%+v", s)
	}
}
