package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/audit"
	"ifri-admissions/services/api/internal/modules/review"
	"ifri-admissions/services/api/internal/platform/database"
)

const devUsage = `usage (development and test only):
  api dev synthetic-applications --count 5000 [--campaign DEV_LOCAL] [--program IA,GL | --exclude-program IA] --confirm
  api dev close-campaign [--campaign DEV_LOCAL] --confirm
Submitted applications are immutable: in a development database they remain until its volume is recreated.
Closing a campaign ends its application period (required before a ranking freeze).`

func codes(value string) []string {
	out := []string{}
	for _, code := range strings.Split(value, ",") {
		if code = strings.ToUpper(strings.TrimSpace(code)); code != "" {
			out = append(out, code)
		}
	}
	return out
}

// devCommand creates synthetic data or closes a campaign; refused in production.
func devCommand(args []string) int {
	if len(args) == 0 || (args[0] != "synthetic-applications" && args[0] != "close-campaign") {
		fmt.Fprintln(os.Stderr, devUsage)
		return 2
	}
	flags := flag.NewFlagSet("dev "+args[0], flag.ContinueOnError)
	count := flags.Int("count", 5000, "number of submitted applications")
	campaign := flags.String("campaign", "DEV_LOCAL", "campaign code")
	only := flags.String("program", "", "comma-separated program codes to generate (default: all)")
	except := flags.String("exclude-program", "", "comma-separated program codes to leave out")
	confirm := flags.Bool("confirm", false, "confirm an irreversible change of the development data")
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
		fmt.Fprintln(os.Stderr, "Refused: development commands never run in production.")
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
	if args[0] == "close-campaign" {
		return closeCampaign(ctx, pool, *campaign)
	}
	started := time.Now()
	n, err := review.GenerateSynthetic(ctx, pool, review.Synthetic{Campaign: *campaign, Count: *count, Programs: codes(*only), Except: codes(*except)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Synthetic generation failed:", err)
		return 1
	}
	fmt.Printf("%d synthetic submitted applications created in %.1f s (no real personal data, no stored PDF).\n", n, time.Since(started).Seconds())
	return 0
}

// closeCampaign ends the application period of a development campaign, audited as SYSTEM.
func closeCampaign(ctx context.Context, pool *pgxpool.Pool, campaign string) int {
	tx, err := pool.Begin(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL unavailable")
		return 1
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var id string
	if err = tx.QueryRow(ctx, `UPDATE admission_campaigns SET status='CLOSED',updated_at=now() WHERE code=$1 AND status='OPEN' RETURNING id::text`, campaign).Scan(&id); err != nil {
		fmt.Fprintln(os.Stderr, "No open campaign with this code.")
		return 1
	}
	if err = audit.Record(ctx, tx, "DEV_CAMPAIGN_CLOSED", audit.Actor{Type: audit.ActorSystem}, "CAMPAIGN", id, map[string]any{"campaign": campaign}); err != nil {
		fmt.Fprintln(os.Stderr, "Audit failed:", err)
		return 1
	}
	if err = tx.Commit(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "Commit failed")
		return 1
	}
	fmt.Printf("Campaign %s closed: no more submissions; rankings can be frozen.\n", campaign)
	return 0
}
