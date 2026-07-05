package main

import (
	"fmt"
	"os"

	"dbtool/cmd"
	_ "dbtool/internal/driver/postgres"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
