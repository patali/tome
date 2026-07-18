package main

import (
	"flag"
	"fmt"

	"github.com/patali/tome/server/internal/auth"
	"github.com/patali/tome/server/internal/store"
)

// runInitAdmin bootstraps (or, with --rotate-key, recovers) the admin account
// with direct database access — in a container: `container exec <name> tome
// init-admin ...`. The API key is printed exactly once; it is never logged by
// the server.
func runInitAdmin(args []string) {
	fs := flag.NewFlagSet("init-admin", flag.ExitOnError)
	email := fs.String("email", "", "admin email address")
	kindleEmail := fs.String("kindle", "", "admin's @kindle.com address")
	dir := fs.String("data-dir", dataDir(), "data directory (default: TOME_DATA_DIR or ./data)")
	rotate := fs.Bool("rotate-key", false, "generate and print a new key for the existing admin (lost-key recovery)")
	_ = fs.Parse(args)

	st, err := store.Open(*dir)
	if err != nil {
		fatal("open store: %v", err)
	}
	defer st.Close()

	if *rotate {
		admin, err := st.FirstAdmin()
		if err != nil {
			fatal("no admin account to rotate (run init-admin without --rotate-key first)")
		}
		key, hash, prefix := auth.NewAPIKey()
		if err := st.RotateKey(admin.ID, hash, prefix); err != nil {
			fatal("rotate: %v", err)
		}
		printKey(admin.Email, key)
		return
	}

	if *email == "" || *kindleEmail == "" {
		fatal("both --email and --kindle are required")
	}
	if exists, err := st.AdminExists(); err != nil {
		fatal("check admin: %v", err)
	} else if exists {
		fatal("an admin account already exists (use --rotate-key if you lost its API key)")
	}

	key, hash, prefix := auth.NewAPIKey()
	if _, err := st.CreateUser(*email, *kindleEmail, hash, prefix, true); err != nil {
		fatal("create admin: %v", err)
	}
	printKey(*email, key)
}

func printKey(email, key string) {
	fmt.Printf(`
Admin account: %s

  API key: %s

This key is shown ONCE and stored only as a hash — save it now (paste it into
the extension's Server settings, and export TOME_ADMIN_KEY for the admin CLI).
`, email, key)
}
