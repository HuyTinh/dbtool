package cmd

import (
	"github.com/spf13/cobra"
)

var pitrCmd = &cobra.Command{
	Use:   "pitr",
	Short: "Point-in-Time Recovery (PITR) management",
	Long: `Manage Point-in-Time Recovery for PostgreSQL databases.

PITR allows you to restore a database to any specific point in time,
provided you have base backups and WAL archives.

Subcommands:
  setup     - Configure WAL archiving for a profile
  backup    - Create a base backup
  restore   - Restore database to a specific point in time
  status    - Show PITR status and available backups
  cleanup   - Remove old backups and WAL files`,
}

func init() {
	RootCmd.AddCommand(pitrCmd)
}
