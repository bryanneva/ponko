package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/bryanneva/ponko/internal/budget"
	"github.com/bryanneva/ponko/internal/config"
)

var budgetCmd = &cobra.Command{
	Use:   "budget",
	Short: "Show spend and remaining allowance",
	RunE:  runBudget,
}

func init() {
	budgetCmd.Flags().String("date", "", "date to query (YYYY-MM-DD, default: today)")
	rootCmd.AddCommand(budgetCmd)
}

func runBudget(cmd *cobra.Command, _ []string) error {
	cfgPath := viper.GetString("config")

	db, err := openDB(viper.GetString("db"))
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	cfg, err := config.Load(cfgPath)
	if err != nil {
		cfg = &config.Config{Budget: config.DefaultBudget()}
	}

	ctrl := budget.NewController(db, cfg.Budget)
	ctx := context.Background()

	date, _ := cmd.Flags().GetString("date")
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}

	daySpent, err := ctrl.DaySpent(ctx, date)
	if err != nil {
		return fmt.Errorf("day spent: %w", err)
	}

	fmt.Printf("Date:          %s\n", date)
	fmt.Printf("Day spent:     $%.4f\n", daySpent)
	fmt.Printf("Day limit:     $%.2f\n", cfg.Budget.PerDayUSD)
	fmt.Printf("Day remaining: $%.4f\n", max(0, cfg.Budget.PerDayUSD-daySpent))
	return nil
}

