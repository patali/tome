// Command tome is the Tome server and its admin tooling.
//
//	tome [serve]        run the HTTP server (default)
//	tome init-admin     bootstrap the admin account (direct DB access)
//	tome admin ...      manage invites/users/settings over the HTTP admin API
package main

import (
	"fmt"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		runServe(args)
	case "init-admin":
		runInitAdmin(args)
	case "admin":
		runAdminCLI(args)
	case "help", "-h", "--help":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w *os.File) {
	fmt.Fprint(w, `tome — web articles to your Kindle

Usage:
  tome [serve]                  run the server (env: TOME_PORT, TOME_DATA_DIR)
  tome init-admin --email E --kindle K [--data-dir D] [--rotate-key]
                                create the admin account and print its API key
                                (--rotate-key: print a NEW key for a lost one)
  tome admin [--server URL] [--key K] <noun> <verb> [flags]
      invites  create [--email HINT] [--ttl 168h] [--send] | list | delete CODE
      users    list | disable ID | enable ID | rotate-key ID
      settings get | set [--resend-api-key K] [--resend-from ADDR]
                                (env: TOME_SERVER_URL, TOME_ADMIN_KEY)
`)
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "tome: "+format+"\n", a...)
	os.Exit(1)
}
