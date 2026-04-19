package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Write a default routing.yaml to the current directory",
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

const defaultRoutingYAML = `# ponko-runner routing configuration
# See: https://github.com/bryanneva/ponko-runtime

max_active_tasks: 3
intake_label: ponko-runner:ready

state_labels:
  in_progress: ponko-runner:in-progress
  blocked: ponko-runner:blocked
  done: ponko-runner:done

repos:
  - owner: your-org
    name: your-repo
    path: ~/repos/your-repo

rules:
  - match:
      labels: [workflow:ralph]
    workflow: ralph
  - match:
      labels: [workflow:fix]
    workflow: fix
  - match:
      labels: [size:small]
    workflow: fix
  - match:
      labels: [size:large]
    workflow: ralph
  - match:
      labels: []
    workflow: rpi

workflows:
  fix:
    runtime: claude-code
    phases:
      - name: implement
        skill: fix
      - name: wrap-up
        skill: wrap-up

  rpi:
    runtime: claude-code
    phases:
      - name: research
        skill: rpi-research
      - name: plan
        skill: rpi-plan
        gate: plan-approved
        model: opus
      - name: implement
        skill: rpi-implement
      - name: wrap-up
        skill: wrap-up

  ralph:
    runtime: claude-code
    phases:
      - name: prd
        skill: prd
      - name: implement
        skill: ralph-ship
      - name: wrap-up
        skill: wrap-up

budget:
  per_run_usd: 5.00
  per_day_usd: 20.00
  per_task_usd: 3.00

gates:
  plan-approved: ponko-runner:plan-approved
`

func runInit(_ *cobra.Command, _ []string) error {
	const target = "routing.yaml"
	if _, err := os.Stat(target); err == nil {
		fmt.Printf("%s already exists. Overwrite? [y/N] ", target)
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		if !strings.EqualFold(strings.TrimSpace(scanner.Text()), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := os.WriteFile(target, []byte(defaultRoutingYAML), 0o644); err != nil {
		return fmt.Errorf("write routing.yaml: %w", err)
	}
	fmt.Printf("Wrote %s\n", target)
	return nil
}
