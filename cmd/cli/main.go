package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var rootCmd = &cobra.Command{
	Use:   "cli",
	Short: "ponko cli — local GitHub issue orchestrator",
	Long:  "ponko cli polls GitHub Issues, routes them through YAML workflows, and invokes Claude Code per phase.",
}

func init() {
	rootCmd.PersistentFlags().String("config", "./routing.yaml", "path to routing config file")
	rootCmd.PersistentFlags().String("db", "~/.ponko-runner/orchestrator.db", "path to SQLite database")
	rootCmd.PersistentFlags().String("events", "~/.ponko-runner/events.jsonl", "path to JSONL event log")
	rootCmd.PersistentFlags().Bool("verbose", false, "enable verbose logging")

	viper.SetEnvPrefix("PONKO_RUNNER")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	_ = viper.BindPFlag("config", rootCmd.PersistentFlags().Lookup("config"))
	_ = viper.BindPFlag("db", rootCmd.PersistentFlags().Lookup("db"))
	_ = viper.BindPFlag("events", rootCmd.PersistentFlags().Lookup("events"))
	_ = viper.BindPFlag("verbose", rootCmd.PersistentFlags().Lookup("verbose"))
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
