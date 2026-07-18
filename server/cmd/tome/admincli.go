package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// runAdminCLI is a thin HTTP client for the /admin API, so administration
// works from anywhere the server is reachable (no DB access needed).
func runAdminCLI(args []string) {
	fs := flag.NewFlagSet("admin", flag.ExitOnError)
	server := fs.String("server", envOr("TOME_SERVER_URL", "http://localhost:8080"), "server URL")
	key := fs.String("key", os.Getenv("TOME_ADMIN_KEY"), "admin API key")
	_ = fs.Parse(args)
	rest := fs.Args()

	if *key == "" {
		fatal("admin API key required (--key or TOME_ADMIN_KEY)")
	}
	if len(rest) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	c := adminClient{base: strings.TrimRight(*server, "/"), key: *key}
	noun, verb, rest := rest[0], rest[1], rest[2:]

	switch noun + " " + verb {
	case "invites create":
		sub := flag.NewFlagSet("invites create", flag.ExitOnError)
		hint := sub.String("email", "", "who this invite is for (required with --send)")
		ttl := sub.Duration("ttl", 168*time.Hour, "invite validity")
		send := sub.Bool("send", false, "email the code to --email via Resend")
		_ = sub.Parse(rest)
		out := c.do("POST", "/admin/invites", map[string]any{
			"emailHint": *hint, "ttlHours": int(ttl.Hours()), "send": *send,
		})
		fmt.Printf("invite: %s\nexpires: %s\n", out["code"], out["expiresAt"])
		if *send {
			if out["emailed"] == true {
				fmt.Printf("emailed to %s\n", *hint)
			} else {
				fmt.Printf("NOT emailed: %v\n", out["emailError"])
			}
		}

	case "invites list":
		out := c.do("GET", "/admin/invites", nil)
		rows, _ := out["invites"].([]any)
		fmt.Printf("%-32s %-24s %-22s %s\n", "CODE", "FOR", "EXPIRES", "USED BY")
		for _, r := range rows {
			m := r.(map[string]any)
			used := "-"
			if m["usedBy"] != nil {
				used = fmt.Sprintf("%v (%v)", m["usedBy"], m["usedAt"])
			}
			fmt.Printf("%-32v %-24v %-22v %s\n", m["code"], m["emailHint"], m["expiresAt"], used)
		}

	case "invites delete":
		requireArg(rest, "invite code")
		c.do("DELETE", "/admin/invites/"+rest[0], nil)
		fmt.Println("deleted")

	case "users list":
		out := c.do("GET", "/admin/users", nil)
		rows, _ := out["users"].([]any)
		fmt.Printf("%-4s %-28s %-28s %-12s %-6s %s\n", "ID", "EMAIL", "KINDLE", "KEY", "ADMIN", "STATE")
		for _, r := range rows {
			m := r.(map[string]any)
			state := "active"
			if m["disabled"] == true {
				state = "disabled"
			}
			fmt.Printf("%-4v %-28v %-28v %-12v %-6v %s\n",
				m["id"], m["email"], m["kindleEmail"], fmt.Sprintf("%v…", m["keyPrefix"]), m["isAdmin"], state)
		}

	case "users disable", "users enable", "users rotate-key":
		requireArg(rest, "user id")
		out := c.do("POST", "/admin/users/"+rest[0]+"/"+verbPath(verb), nil)
		if verb == "rotate-key" {
			fmt.Printf("new API key (shown once): %s\n", out["apiKey"])
		} else {
			fmt.Println("ok")
		}

	case "settings get":
		out := c.do("GET", "/admin/settings", nil)
		fmt.Printf("resend from:    %v\nresend api key: set=%v\n", out["resendFrom"], out["resendApiKeySet"])

	case "settings set":
		sub := flag.NewFlagSet("settings set", flag.ExitOnError)
		apiKey := sub.String("resend-api-key", "", "Resend API key (re_...)")
		from := sub.String("resend-from", "", "verified sender address, e.g. tome@yourdomain.com")
		_ = sub.Parse(rest)
		body := map[string]any{}
		if *apiKey != "" {
			body["resendApiKey"] = *apiKey
		}
		if *from != "" {
			body["resendFrom"] = *from
		}
		if len(body) == 0 {
			fatal("nothing to set (use --resend-api-key / --resend-from)")
		}
		out := c.do("PUT", "/admin/settings", body)
		fmt.Printf("resend from:    %v\nresend api key: set=%v\n", out["resendFrom"], out["resendApiKeySet"])

	default:
		fmt.Fprintf(os.Stderr, "unknown admin command %q %q\n\n", noun, verb)
		usage(os.Stderr)
		os.Exit(2)
	}
}

func verbPath(verb string) string { return verb } // disable | enable | rotate-key match URL parts

func requireArg(rest []string, what string) {
	if len(rest) < 1 {
		fatal("missing %s", what)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type adminClient struct {
	base, key string
}

func (c adminClient) do(method, path string, body any) map[string]any {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			fatal("%v", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, rdr)
	if err != nil {
		fatal("%v", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		fatal("request: %v", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if e, ok := out["error"].(string); ok {
			msg = e
		}
		fatal("%s %s -> %s: %s", method, path, resp.Status, msg)
	}
	return out
}
