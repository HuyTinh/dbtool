package driver

import (
	"context"

	"dbtool/internal/config"
)

type Format string

const (
	FormatCustom    Format = "custom"
	FormatPlain     Format = "plain"
	FormatDirectory Format = "directory"
	FormatUnknown   Format = "unknown"
)

type Progress struct {
	Percent float64
	Message string
	Done    bool
	Err     error
}

type RestoreOptions struct {
	Profile       config.Profile
	FilePath      string
	Format        Format
	Jobs          int
	Clean         bool
	IncludeTable  []string
	ExcludeTable  []string
	IncludeSchema []string
	ExcludeSchema []string
	DryRun        bool
}

type DumpOptions struct {
	Profile       config.Profile
	FilePath      string
	Format        Format
	IncludeTable  []string
	ExcludeTable  []string
	IncludeSchema []string
	ExcludeSchema []string
}

type Severity int

const (
	CheckOK Severity = iota
	CheckWarning
	CheckError
)

type DoctorCheck struct {
	Name     string
	Severity Severity
	OK       bool
	Message  string
	Hint     string
}

type Driver interface {
	Name() string // "postgres", "mysql", "mongodb"
	DetectFormat(filePath string) (Format, error)
	Restore(ctx context.Context, opts RestoreOptions) (<-chan Progress, error)
	Dump(ctx context.Context, opts DumpOptions) (<-chan Progress, error)
	TestConnection(ctx context.Context, profile config.Profile) error // takes ctx to support timeout
	Doctor(profile *config.Profile) []DoctorCheck                     // profile == nil: check binaries/global, profile != nil: check connection as well
}
