package hobalight

import (
	"log"
)

type Config struct {
	TrashRetentionDays int `yaml:"trash_retention_days,omitempty"`
	path               string
	sources            map[string]string
	Exclude            []string `yaml:"exclude,omitempty"`
	Workers            int      `yaml:"workers,omitempty"`
	Storage            string   `yaml:"storage"`
	Directories        []string `yaml:"directories"`
}
type Identity struct {
	Error    string  `yaml:"error,omitempty"`
	Path     string  `yaml:"path"`
	Size     int64   `yaml:"size"`
	Created  *string `yaml:"created"`
	Modified string  `yaml:"modified"`
}
type File struct {
	Identity `yaml:",inline"`
	Blob     string `yaml:"blob,omitempty"`
}
type Session struct {
	Version   string   `yaml:"version,omitempty"`
	Platform  string   `yaml:"platform,omitempty"`
	Directory string   `yaml:"directory"`
	Started   string   `yaml:"started"`
	Files     []string `yaml:"files"`
	Errors    []string `yaml:"errors,omitempty"`
}
type ReferenceFile struct {
	Failed bool   `yaml:"failed,omitempty"`
	ID     string `yaml:"id"`
	Path   string `yaml:"path"`
	Size   int64  `yaml:"size"`
}

const referenceFormat = 1

type Reference struct {
	Format    int    `yaml:"format,omitempty"`
	Directory string `yaml:"directory"`
	Started   string `yaml:"started"`
	Session   string `yaml:"session"`
	// nil means a legacy reference; a pointer to an empty slice means an indexed empty session.
	Files *[]ReferenceFile `yaml:"files,omitempty"`
}
type Store struct {
	backend            storageBackend
	trashRetentionDays int
	sessionListings    map[string]Reference
	excludes           []excludeRule
	workers            int
	logger             *log.Logger
	progress           func(progressUpdate)
}
