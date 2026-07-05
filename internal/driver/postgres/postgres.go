package postgres

import (
	"dbtool/internal/cache"
	"dbtool/internal/driver"
)

type PostgresDriver struct {
	cache *cache.FileCache
}

func (d *PostgresDriver) Name() string {
	return "postgres"
}

func init() {
	fc, err := cache.NewFileCache()
	if err != nil {
		// Fallback gracefully without cache if initialization fails
		driver.Register(&PostgresDriver{cache: nil})
		return
	}
	driver.Register(&PostgresDriver{cache: fc})
}
