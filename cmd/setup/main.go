package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var configPath string

func main() {
	root := &cobra.Command{
		Use:   "ponko-setup",
		Short: "Ponko setup wizard",
		Long:  "Interactive setup, deploy, and validation for Ponko.",
		RunE:  runSetup,
	}

	root.PersistentFlags().StringVarP(&configPath, "config", "c", "ponko.yaml", "path to config file")

	root.AddCommand(&cobra.Command{
		Use:   "deploy",
		Short: "Deploy from existing ponko.yaml",
		RunE:  runDeploy,
	})

	root.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Check config and health",
		RunE:  runValidate,
	})

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runSetup(_ *cobra.Command, _ []string) error {
	cfg := defaultConfig()

	if err := collectRequired(cfg); err != nil {
		return err
	}

	cfg.Deploy.APIKey = generateSecret()
	cfg.Deploy.CookieSigningKey = generateSecret()
	fmt.Println("Generated API key and cookie signing key.")

	if err := collectOptional(cfg); err != nil {
		return err
	}

	return deploy(cfg)
}

func runDeploy(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	return deploy(cfg)
}

func deploy(cfg *Config) error {
	switch cfg.Deploy.Platform {
	case platformFly:
		return deployFly(cfg)
	case platformDocker:
		return deployDocker(cfg)
	default:
		return fmt.Errorf("unknown platform: %s", cfg.Deploy.Platform)
	}
}

func runValidate(_ *cobra.Command, _ []string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	return validate(cfg)
}
