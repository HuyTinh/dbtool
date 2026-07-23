package driver

import (
	"context"
	"time"

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

// CatalogTable is a user-visible table that can be selected for an operation.
type CatalogTable struct {
	Schema string
	Name   string
}

// CatalogLister is an optional driver capability for interactive object selection.
// Drivers that do not implement it retain the text-pattern filter workflow.
type CatalogLister interface {
	ListSchemas(ctx context.Context, profile config.Profile) ([]string, error)
	ListTables(ctx context.Context, profile config.Profile, schemas []string) ([]CatalogTable, error)
}

// CatalogColumnLister is an optional extension for type-ahead column selectors.
// It is intentionally separate from CatalogLister so existing drivers retain
// table browsing without needing to implement column enumeration.
type CatalogColumnLister interface {
	ListColumns(ctx context.Context, profile config.Profile, schema, table string) ([]string, error)
}

// CatalogColumnSearchResult is a bounded, user-facing field match for guided
// schema changes. It intentionally contains identifiers and type only; values
// and connection details are never returned by catalog search.
type CatalogColumnSearchResult struct {
	Schema string
	Table  string
	Column string
	Type   string
}

// CatalogColumnSearcher is an optional server-side search capability for
// intent-first schema editors. Drivers that do not implement it retain the
// manual schema/table workflow.
type CatalogColumnSearcher interface {
	SearchColumns(ctx context.Context, profile config.Profile, query string, limit int) ([]CatalogColumnSearchResult, error)
}

type RowCountCollector interface {
	CollectRowCounts(ctx context.Context, profile config.Profile, tables []CatalogTable) ([]TableRowCount, error)
}

type HealthCapability struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Message   string `json:"message,omitempty"`
}

// HealthSnapshot contains read-only, point-in-time database observations.
type HealthSnapshot struct {
	ServerVersion      string             `json:"server_version"`
	Latency            time.Duration      `json:"-"`
	DatabaseSize       int64              `json:"database_size"`
	CurrentConnections int                `json:"current_connections"`
	MaxConnections     int                `json:"max_connections"`
	ActiveConnections  int                `json:"active_connections"`
	IdleConnections    int                `json:"idle_connections"`
	IdleInTransaction  int                `json:"idle_in_transaction"`
	LongRunningQueries int                `json:"long_running_queries"`
	BlockedSessions    int                `json:"blocked_sessions"`
	Capabilities       []HealthCapability `json:"capabilities"`
}

// HealthCollector is an optional read-only database health capability.
type HealthCollector interface {
	CollectHealth(ctx context.Context, profile config.Profile, longQueryThreshold time.Duration) (*HealthSnapshot, error)
}

// SizeRelation describes a user relation and its PostgreSQL storage footprint.
type SizeRelation struct {
	Schema        string `json:"schema"`
	Name          string `json:"name"`
	EstimatedRows int64  `json:"estimated_rows"`
	TableBytes    int64  `json:"table_bytes"`
	IndexBytes    int64  `json:"index_bytes"`
	ToastBytes    int64  `json:"toast_bytes"`
	TotalBytes    int64  `json:"total_bytes"`
}

type SizeIndex struct {
	Schema    string `json:"schema"`
	Name      string `json:"name"`
	TableName string `json:"table_name"`
	SizeBytes int64  `json:"size_bytes"`
}

// SizeSnapshot contains read-only PostgreSQL storage metadata. EstimatedRows
// comes from catalog statistics and is not an exact row count.
type SizeSnapshot struct {
	DatabaseSize int64          `json:"database_size"`
	Relations    []SizeRelation `json:"relations"`
	Indexes      []SizeIndex    `json:"indexes"`
}

// SizeCollector is an optional read-only relation and index storage capability.
type SizeCollector interface {
	CollectSize(ctx context.Context, profile config.Profile, schema string, limit int) (*SizeSnapshot, error)
}

// SessionEntry is a redacted, read-only PostgreSQL backend observation. It
// intentionally excludes query text and connection details that may expose secrets.
type SessionEntry struct {
	PID           int32   `json:"pid"`
	Database      string  `json:"database"`
	User          string  `json:"user"`
	State         string  `json:"state"`
	WaitEventType string  `json:"wait_event_type,omitempty"`
	WaitEvent     string  `json:"wait_event,omitempty"`
	QueryAgeMS    int64   `json:"query_age_ms"`
	BlockingPIDs  []int32 `json:"blocking_pids"`
}

// SessionSnapshot contains read-only PostgreSQL session metadata. Query text
// and connection strings are never collected or rendered.
type SessionSnapshot struct {
	SelfPID  int32          `json:"self_pid,omitempty"`
	Sessions []SessionEntry `json:"sessions"`
}

// SessionCollector is an optional read-only database session capability.
type SessionCollector interface {
	CollectSessions(ctx context.Context, profile config.Profile, state string, limit int) (*SessionSnapshot, error)
}

type Driver interface {
	Name() string // "postgres", "mysql", "mongodb"
	DetectFormat(filePath string) (Format, error)
	Restore(ctx context.Context, opts RestoreOptions) (<-chan Progress, error)
	Dump(ctx context.Context, opts DumpOptions) (<-chan Progress, error)
	TestConnection(ctx context.Context, profile config.Profile) error // takes ctx to support timeout
	Doctor(profile *config.Profile) []DoctorCheck                     // profile == nil: check binaries/global, profile != nil: check connection as well
	EnsureDatabaseExists(ctx context.Context, profile config.Profile) error
	EnsureSchemas(ctx context.Context, profile config.Profile, schemas []string) error
	Optimize(ctx context.Context, profile config.Profile) error
	ValidateDump(ctx context.Context, opts ValidateOptions) (*ValidationResult, error)
	VerifyRestore(ctx context.Context, opts VerifyRestoreOptions) (*VerifyResult, error)
}
