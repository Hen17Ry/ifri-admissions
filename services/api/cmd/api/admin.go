package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"ifri-admissions/services/api/internal/config"
	"ifri-admissions/services/api/internal/modules/admin"
	"ifri-admissions/services/api/internal/platform/database"
	"ifri-admissions/services/api/internal/platform/passwords"
	"ifri-admissions/services/api/internal/platform/ratelimit"
	"ifri-admissions/services/api/internal/platform/secrets"
)

// newAdminService assembles the admin module; limiter is nil for server-side commands.
func newAdminService(c config.Config, logger *slog.Logger, limiter ratelimit.Limiter) (*admin.Service, error) {
	if _, err := passwords.CommonCount(); err != nil {
		return nil, fmt.Errorf("common password list unavailable")
	}
	keys, err := secrets.Derive(c.AdminSecretsKey, c.AdminSecretsKeyVersion)
	if err != nil {
		return nil, err
	}
	// At most two argon2id computations at once: 2 × 64 MiB.
	hasher, err := passwords.NewHasher(passwords.Default, 2)
	if err != nil {
		return nil, err
	}
	return &admin.Service{Keys: keys, Hasher: hasher, Limiter: limiter, Logger: logger, MFARequired: c.AdminMFARequired,
		Idle: c.AdminSessionIdle, Absolute: c.AdminSessionAbsolute, ActivationTTL: c.AdminActivationTTL, Origin: c.FrontendOrigin}, nil
}

const adminUsage = `usage (on the server only):
  api admin create-super-admin --email EMAIL --name NAME   password read from the terminal or stdin
  api admin reset-mfa --email EMAIL                        lost phone and recovery codes
  api admin reset-password --email EMAIL                   forgotten password: prints a new activation link
  api admin reactivate --email EMAIL                       re-enable a disabled account`

// adminCommand runs a server-side account command; every action is audited as SYSTEM.
func adminCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, adminUsage)
		return 2
	}
	flags := flag.NewFlagSet("admin "+args[0], flag.ContinueOnError)
	email := flags.String("email", "", "account email")
	name := flags.String("name", "", "display name (create-super-admin)")
	if err := flags.Parse(args[1:]); err != nil || *email == "" || flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, adminUsage)
		return 2
	}
	c, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Configuration:", err)
		return 1
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil)).With("service", "api-admin-cli")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, c.DatabaseURL())
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL configuration failed")
		return 1
	}
	defer pool.Close()
	service, err := newAdminService(c, logger, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	service.Pool = pool
	switch args[0] {
	case "create-super-admin":
		password, readErr := readPassword()
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			return 1
		}
		if _, err = service.BootstrapSuperAdmin(ctx, *email, *name, password); err == nil {
			fmt.Println("Super-administrator created. Sign in at /admin to enrol the second factor.")
		}
	case "reset-mfa":
		if err = service.ResetMFAByEmail(ctx, *email); err == nil {
			fmt.Println("Second factor reset; sessions revoked. Enrolment is required at the next sign-in.")
		}
	case "reset-password":
		url, expires, resetErr := service.ResetPasswordByEmail(ctx, *email)
		if err = resetErr; err == nil {
			fmt.Printf("Password cleared; sessions revoked. Single-use activation link (expires %s), to transmit securely:\n%s\n", expires.Format(time.RFC3339), url)
		}
	case "reactivate":
		if err = service.ReactivateByEmail(ctx, *email); err == nil {
			fmt.Println("Account re-enabled.")
		}
	default:
		fmt.Fprintln(os.Stderr, adminUsage)
		return 2
	}
	if err != nil {
		var p *admin.Problem
		if errors.As(err, &p) {
			fmt.Fprintln(os.Stderr, p.Message)
		} else {
			fmt.Fprintln(os.Stderr, "Refused:", err)
		}
		return 1
	}
	return 0
}

// readPassword never takes the password from an argument: hidden prompt on a terminal,
// otherwise the first line of stdin (scripts, `docker compose exec -T`).
func readPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		fmt.Fprint(os.Stderr, "Password (12 characters or more): ")
		first, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		fmt.Fprint(os.Stderr, "Confirm password: ")
		second, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		if string(first) != string(second) {
			return "", errors.New("passwords differ")
		}
		return string(first), nil
	}
	line, err := bufio.NewReader(io.LimitReader(os.Stdin, 1024)).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
