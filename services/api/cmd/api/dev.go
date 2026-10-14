package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/review"
	"ifri-admissions/services/api/internal/platform/database"
)

const devUsage = `usage (development and test only):
  api dev synthetic-applications --count 5000 [--campaign DEV_LOCAL] --confirm
Submitted applications are immutable: in a development database they remain until its volume is recreated.`

// devCommand creates synthetic data; refused in production.
func devCommand(args []string) int {
	if len(args) == 0 || args[0] != "synthetic-applications" {
		fmt.Fprintln(os.Stderr, devUsage)
		return 2
	}
	flags := flag.NewFlagSet("dev synthetic-applications", flag.ContinueOnError)
	count := flags.Int("count", 5000, "number of submitted applications")
	campaign := flags.String("campaign", "DEV_LOCAL", "campaign code")
	confirm := flags.Bool("confirm", false, "confirm that submitted data cannot be removed")
	if err := flags.Parse(args[1:]); err != nil || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, devUsage)
		return 2
	}
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Configuration:", err)
		return 1
	}
	if c.Environment == "production" {
		fmt.Fprintln(os.Stderr, "Refused: synthetic data is never generated in production.")
		return 1
	}
	if !*confirm {
		fmt.Fprintln(os.Stderr, devUsage)
		fmt.Fprintln(os.Stderr, "Add --confirm to proceed.")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, c.DatabaseURL())
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL configuration failed")
		return 1
	}
	defer pool.Close()
	started := time.Now()
	n, err := review.GenerateSynthetic(ctx, pool, *campaign, *count)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Synthetic generation failed:", err)
		return 1
	}
	fmt.Printf("%d synthetic submitted applications created in %.1f s (no real personal data, no stored PDF).\n", n, time.Since(started).Seconds())
	return 0
}
