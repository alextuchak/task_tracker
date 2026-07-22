package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"task_tracker/internal"
	"task_tracker/internal/identity"
	"task_tracker/internal/infrastructure/persistence"
	"task_tracker/internal/service"
	"time"
)

const usage = `usage: cli <command> [flags]

commands:
  grant-admin --email <email>   выдать пользователю глобальную роль admin
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "grant-admin":
		if err := grantAdmin(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "grant-admin:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func grantAdmin(args []string) error {
	fs := flag.NewFlagSet("grant-admin", flag.ExitOnError)
	email := fs.String("email", "", "email пользователя")
	_ = fs.Parse(args)
	if *email == "" {
		return fmt.Errorf("--email is required")
	}

	svc, cleanup, err := buildAuthService()
	if err != nil {
		return err
	}
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := svc.GrantAdmin(ctx, *email); err != nil {
		return err
	}
	fmt.Printf("user %s is now admin\n", *email)
	return nil
}

func buildAuthService() (*service.Auth, func(), error) {
	cfg, err := internal.NewConfig()
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("config validate: %w", err)
	}
	db, err := persistence.NewMySQL(cfg.MySQL)
	if err != nil {
		return nil, nil, fmt.Errorf("mysql: %w", err)
	}
	svc := service.NewAuth(persistence.NewUserRepo(db), identity.NewProvider(cfg.Auth))
	return svc, func() { _ = db.Close() }, nil
}
