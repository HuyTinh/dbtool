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
	FormatTar       Format = "tar"
	FormatUnknown   Format = "unknown"
)

type Progress struct {
	Percent float64
	Message string
	Done    bool
	Err     error
}

type RestoreOptions struct {
	Profile         config.Profile
	FilePath        string
	Format          Format
	Jobs            int
	Clean           bool
	IncludeTable    []string
	ExcludeTable    []string
	IncludeSchema   []string
	ExcludeSchema   []string
	DryRun          bool
	CreateIfMissing bool
}

type DumpOptions struct {
	Profile       config.Profile
	FilePath      string
	Format        Format
	SchemaOnly    bool
	DataOnly      bool
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

type ValidateOptions struct {
	FilePath      string
	Format        Format
	Profile       config.Profile
	IncludeTable  []string
	ExcludeTable  []string
	IncludeSchema []string
	ExcludeSchema []string
}

type ValidationResult struct {
	TableCount       int
	ViewCount        int
	SchemaNames      []string
	Warnings         []string
	Errors           []string
	SchemaCompatible bool
	DumpPGVersion    string
	TargetPGVersion  string
	FilterMatches    FilterMatchResult
	HasTOC           bool
}

type FilterMatchResult struct {
	IncludeTableMatched    []string
	IncludeTableUnmatched  []string
	ExcludeTableMatched    []string
	ExcludeTableUnmatched  []string
	IncludeSchemaMatched   []string
	IncludeSchemaUnmatched []string
	ExcludeSchemaMatched   []string
	ExcludeSchemaUnmatched []string
}

type VerifyRestoreOptions struct {
	Profile       config.Profile
	FilePath      string
	Format        Format
	IncludeTable  []string
	ExcludeTable  []string
	IncludeSchema []string
	ExcludeSchema []string
}

type VerifyResult struct {
	Verified       bool
	TablesExpected int
	TablesFound    int
	MissingTables  []string
	RowCounts      []TableRowCount
	SampleOK       []string
	SampleFailed   []string
	Warnings       []string
	Errors         []string
}

type TableRowCount struct {
	Schema   string
	Table    string
	RowCount int64
}

type Driver interface {
	Name() string // "postgres", "mysql", "mongodb"
	DetectFormat(filePath string) (Format, error)
	Restore(ctx context.Context, opts RestoreOptions) (<-chan Progress, error)
	Dump(ctx context.Context, opts DumpOptions) (<-chan Progress, error)
	TestConnection(ctx context.Context, profile config.Profile) error // takes ctx to support timeout
	Doctor(profile *config.Profile) []DoctorCheck                     // profile == nil: check binaries/global, profile != nil: check connection as well
	EnsureDatabaseExists(ctx context.Context, profile config.Profile) error
	Optimize(ctx context.Context, profile config.Profile) error
	ValidateDump(ctx context.Context, opts ValidateOptions) (*ValidationResult, error)
	VerifyRestore(ctx context.Context, opts VerifyRestoreOptions) (*VerifyResult, error)
}
