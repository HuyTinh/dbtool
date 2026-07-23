package cmd

import (
	"fmt"
	"strings"

	"dbtool/internal/config"

	"github.com/spf13/cobra"
)

var (
	profileHost                 string
	profilePort                 int
	profileUser                 string
	profileDatabase             string
	profilePassword             string
	profileDriver               string
	profileRestoreDrillSandbox  bool
	profileMigrateSecretsDryRun bool
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "Manage database connection profiles",
}

var profileAddCmd = &cobra.Command{
	Use:   "add [name]",
	Short: "Add a database connection profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		p := config.Profile{
			Driver:              profileDriver,
			Host:                profileHost,
			Port:                profilePort,
			User:                profileUser,
			Database:            profileDatabase,
			Password:            profilePassword,
			RestoreDrillSandbox: profileRestoreDrillSandbox,
		}

		if err := cfg.SaveProfile(name, p); err != nil {
			return fmt.Errorf("failed to save profile %q: %w", name, err)
		}

		fmt.Printf("✓ Profile %q saved successfully.\n", name)
		return nil
	},
}

var profileListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all database connection profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		if len(cfg.Profiles) == 0 {
			fmt.Println("No profiles configured yet. Create one with 'dbtool profile add'.")
			return nil
		}

		fmt.Printf("%-20s %-10s %-25s %-15s %-15s\n", "NAME", "DRIVER", "HOST:PORT", "USER", "DATABASE")
		fmt.Println(strings.Repeat("-", 85))
		for name, p := range cfg.Profiles {
			hostPort := fmt.Sprintf("%s:%d", p.Host, p.Port)
			fmt.Printf("%-20s %-10s %-25s %-15s %-15s\n", name, p.Driver, hostPort, p.User, p.Database)
		}
		return nil
	},
}

var profileMigrateSecretsCmd = &cobra.Command{
	Use:   "migrate-secrets",
	Short: "Move plaintext profile passwords into secure storage",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		if profileMigrateSecretsDryRun {
			fmt.Printf("Would migrate %d plaintext profile password(s).\n", config.PendingProfileSecretMigrations(cfg))
			return nil
		}

		migrated, err := config.MigrateProfileSecrets(cfg)
		if err != nil {
			return fmt.Errorf("migrate profile secrets: %d profile password(s) stored before failure; profiles.yaml was not changed: %w", migrated, err)
		}
		if migrated == 0 {
			fmt.Println("No plaintext profile passwords to migrate.")
			return nil
		}
		if err := config.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save migrated profile secrets: %w", err)
		}

		fmt.Printf("✓ Migrated %d profile password(s) to secure storage.\n", migrated)
		return nil
	},
}

func init() {
	profileAddCmd.Flags().StringVar(&profileDriver, "driver", "postgres", "Database engine driver (e.g. postgres)")
	profileAddCmd.Flags().StringVar(&profileHost, "host", "localhost", "Host address")
	profileAddCmd.Flags().IntVar(&profilePort, "port", 5432, "Host port")
	profileAddCmd.Flags().StringVar(&profileUser, "user", "postgres", "Database username")
	profileAddCmd.Flags().StringVar(&profileDatabase, "db", "", "Database name")
	profileAddCmd.Flags().StringVar(&profilePassword, "password", "", "Database password")
	profileAddCmd.Flags().BoolVar(&profileRestoreDrillSandbox, "restore-drill-sandbox", false, "Explicitly allow this profile to be a destructive restore-drill target")

	_ = profileAddCmd.MarkFlagRequired("db")
	profileMigrateSecretsCmd.Flags().BoolVar(&profileMigrateSecretsDryRun, "dry-run", false, "Report plaintext profile passwords that would be migrated without changing storage")

	profileCmd.AddCommand(profileAddCmd)
	profileCmd.AddCommand(profileListCmd)
	profileCmd.AddCommand(profileMigrateSecretsCmd)
	RootCmd.AddCommand(profileCmd)
}
