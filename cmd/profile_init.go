package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"dbtool/internal/config"
	"dbtool/internal/importer"
	_ "dbtool/internal/importer/dockercompose"
	_ "dbtool/internal/importer/dotenv"
	_ "dbtool/internal/importer/springboot"

	"github.com/spf13/cobra"
)

var profileInitFrom string

var profileInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Detect database connections from local configuration files and initialize profiles",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := profileInitFrom
		if dir == "" {
			dir = "."
		}

		fmt.Printf("Scanning directory %q for configuration files...\n", dir)
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		importers := importer.All()
		detectedCount := 0

		for _, imp := range importers {
			files, err := imp.Detect(dir)
			if err != nil {
				fmt.Printf("Warning: importer %s failed to scan: %v\n", imp.Name(), err)
				continue
			}

			for _, file := range files {
				fmt.Printf("Found config file: %s (%s)\n", file, imp.Name())
				profiles, err := imp.Parse(file)
				if err != nil {
					fmt.Printf("Warning: failed to parse config file %s: %v\n", file, err)
					continue
				}

				for _, p := range profiles {
					detectedCount++
					fmt.Println()
					fmt.Printf("Database Connection Detected:\n")
					fmt.Printf("  Source:   %s\n", p.Source)
					fmt.Printf("  Driver:   %s\n", p.Driver)
					fmt.Printf("  Host:     %s:%d\n", p.Host, p.Port)
					fmt.Printf("  Database: %s\n", p.Database)
					fmt.Printf("  User:     %s\n", p.Username)
					if p.PasswordIsRef {
						fmt.Printf("  Password: (referenced via environment variable, not stored plaintext)\n")
					} else if p.Password != "" {
						fmt.Printf("  Password: ****\n")
					} else {
						fmt.Printf("  Password: (empty)\n")
					}

					if !promptConfirm("Do you want to import this profile?") {
						continue
					}

					profileName := promptString("Enter profile name", p.SuggestedName)
					
					newProfile := config.Profile{
						Driver:   p.Driver,
						Host:     p.Host,
						Port:     p.Port,
						User:     p.Username,
						Database: p.Database,
						Password: p.Password,
					}

					if err := cfg.SaveProfile(profileName, newProfile); err != nil {
						fmt.Printf("Error: failed to save profile %q: %v\n", profileName, err)
					} else {
						fmt.Printf("✓ Profile %q saved successfully!\n", profileName)
					}
				}
			}
		}

		if detectedCount == 0 {
			fmt.Println("No database configuration files detected in this directory.")
		}

		return nil
	},
}

func promptString(prompt, defaultValue string) string {
	fmt.Printf("%s [%s]: ", prompt, defaultValue)
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return defaultValue
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultValue
	}
	return text
}

func promptConfirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	text, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	text = strings.TrimSpace(strings.ToLower(text))
	return text == "y" || text == "yes"
}

func init() {
	profileInitCmd.Flags().StringVar(&profileInitFrom, "from", ".", "Directory to scan for configuration files")
	
	// Register under profile command (dbtool profile init)
	profileCmd.AddCommand(profileInitCmd)
}
