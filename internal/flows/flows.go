// Package flows stores reusable, secret-free operation settings separately from audit history.
package flows

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dbtool/internal/config"
)

const (
	MaxRecent = 20
	MaxPinned = 5
)

type Operation string

const (
	OperationDump    Operation = "dump"
	OperationRestore Operation = "restore"
	OperationMigrate Operation = "migrate"
)

// Settings intentionally contains only non-secret operation controls. Connection
// details are always resolved from the named profile at the time of use.
type Settings struct {
	Format          string   `json:"format,omitempty"`
	Jobs            int      `json:"jobs,omitempty"`
	Clean           bool     `json:"clean,omitempty"`
	CreateIfMissing bool     `json:"create_if_missing,omitempty"`
	Optimize        bool     `json:"optimize,omitempty"`
	SchemaOnly      bool     `json:"schema_only,omitempty"`
	DataOnly        bool     `json:"data_only,omitempty"`
	IncludeTables   []string `json:"include_tables,omitempty"`
	ExcludeTables   []string `json:"exclude_tables,omitempty"`
	IncludeSchemas  []string `json:"include_schemas,omitempty"`
	ExcludeSchemas  []string `json:"exclude_schemas,omitempty"`
}

// Flow is a reusable entry point, never an executable command. It keeps only
// profile names and operation-safe options; profile credentials remain in config.
type Flow struct {
	Operation          Operation `json:"operation"`
	Profile            string    `json:"profile"`
	DestinationProfile string    `json:"destination_profile,omitempty"`
	FilePath           string    `json:"file_path,omitempty"`
	Settings           Settings  `json:"settings,omitempty"`
	LastUsed           time.Time `json:"last_used"`
}

type Data struct {
	Recent []Flow `json:"recent"`
	Pinned []Flow `json:"pinned"`
}

type Store struct{ path string }

func NewStore(path string) *Store { return &Store{path: path} }

func Open() (*Store, error) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return nil, err
	}
	return NewStore(filepath.Join(dir, "flows.json")), nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Load() (Data, error) {
	data := Data{}
	bytes, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return data, nil
	}
	if err != nil {
		return data, fmt.Errorf("read flows: %w", err)
	}
	if err := json.Unmarshal(bytes, &data); err != nil {
		return data, fmt.Errorf("parse flows: %w", err)
	}
	sortFlows(data.Recent)
	sortFlows(data.Pinned)
	return data, nil
}

// Record updates a matching recent flow (and its pinned copy), or adds a new
// recent flow. It never persists profile connection settings or secrets.
func (s *Store) Record(flow Flow) error {
	if err := validate(flow); err != nil {
		return err
	}
	data, err := s.Load()
	if err != nil {
		return err
	}
	flow.LastUsed = time.Now().UTC()
	data.Recent = upsert(data.Recent, flow)
	data.Pinned = updateIfPresent(data.Pinned, flow)
	data.Recent = limit(data.Recent, MaxRecent)
	return s.save(data)
}

// SetPinned pins or unpins an existing reusable flow. Pinning adds a copy, so
// recents and pinned lists have independent limits.
func (s *Store) SetPinned(flow Flow, pinned bool) error {
	if err := validate(flow); err != nil {
		return err
	}
	data, err := s.Load()
	if err != nil {
		return err
	}
	if pinned {
		flow.LastUsed = time.Now().UTC()
		data.Pinned = upsert(data.Pinned, flow)
		data.Pinned = limit(data.Pinned, MaxPinned)
	} else {
		data.Pinned = remove(data.Pinned, flow)
	}
	return s.save(data)
}

func validate(flow Flow) error {
	if flow.Operation != OperationDump && flow.Operation != OperationRestore && flow.Operation != OperationMigrate {
		return fmt.Errorf("unsupported flow operation %q", flow.Operation)
	}
	if flow.Profile == "" {
		return fmt.Errorf("flow profile is required")
	}
	if flow.Operation == OperationMigrate && flow.DestinationProfile == "" {
		return fmt.Errorf("migration destination profile is required")
	}
	return nil
}

func same(a, b Flow) bool {
	return a.Operation == b.Operation && a.Profile == b.Profile && a.DestinationProfile == b.DestinationProfile && a.FilePath == b.FilePath
}

func upsert(flows []Flow, flow Flow) []Flow {
	flows = remove(flows, flow)
	flows = append([]Flow{flow}, flows...)
	sortFlows(flows)
	return flows
}

func updateIfPresent(flows []Flow, flow Flow) []Flow {
	for _, existing := range flows {
		if same(existing, flow) {
			return upsert(flows, flow)
		}
	}
	return flows
}

func remove(flows []Flow, flow Flow) []Flow {
	out := flows[:0]
	for _, existing := range flows {
		if !same(existing, flow) {
			out = append(out, existing)
		}
	}
	return out
}

func limit(flows []Flow, max int) []Flow {
	if len(flows) > max {
		return flows[:max]
	}
	return flows
}

func sortFlows(flows []Flow) {
	sort.SliceStable(flows, func(i, j int) bool { return flows[i].LastUsed.After(flows[j].LastUsed) })
}

func (s *Store) save(data Data) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("create flows directory: %w", err)
	}
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode flows: %w", err)
	}
	bytes = append(bytes, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), filepath.Base(s.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temporary flows file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary flows file: %w", err)
	}
	if _, err := tmp.Write(bytes); err != nil {
		tmp.Close()
		return fmt.Errorf("write flows: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync flows: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close flows: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace flows atomically: %w", err)
	}
	if err := os.Chmod(s.path, 0600); err != nil {
		return fmt.Errorf("secure flows file: %w", err)
	}
	return nil
}
