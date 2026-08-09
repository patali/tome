package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
				warnBase(out)
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

	case "stats show", "stats":
		sub := flag.NewFlagSet("stats", flag.ExitOnError)
		perUser := sub.Bool("per-user", false, "also break down by user")
		_ = sub.Parse(rest)
		path := "/admin/stats"
		if *perUser {
			path += "?perUser=true"
		}
		out := c.do("GET", path, nil)
		n := func(k string) int64 {
			f, _ := out[k].(float64)
			return int64(f)
		}
		fmt.Printf("users         %d total, %d active (30d), %d disabled\n",
			n("users"), n("usersActive30d"), n("usersDisabled"))
		fmt.Printf("conversions   %d today, %d this week, %d this month (%d failed)\n",
			n("conversions1d"), n("conversions7d"), n("conversions30d"), n("failed30d"))
		fmt.Printf("waiting       %d invite requests, %d unused invites\n",
			n("requestsOpen"), n("invitesOpen"))
		if rows, ok := out["perUser"].([]any); ok && len(rows) > 0 {
			fmt.Printf("\n%-28s %-12s %-8s %s\n", "USER", "LAST SEEN", "30d", "FAILED")
			for _, r := range rows {
				m := r.(map[string]any)
				seen, _ := m["lastSeenAt"].(string)
				if seen == "" {
					seen = "never"
				} else if len(seen) >= 10 {
					seen = seen[:10]
				}
				label, _ := m["email"].(string)
				if m["disabled"] == true {
					label += " (disabled)"
				}
				fmt.Printf("%-28s %-12s %-8v %v\n", label, seen, m["conversions"], m["failed"])
			}
		}

	case "conversions list":
		sub := flag.NewFlagSet("conversions list", flag.ExitOnError)
		since := sub.String("since", "7d", "window: 7d, 24h, or an RFC3339 time")
		user := sub.String("user", "", "filter to one user (email or id)")
		limit := sub.Int("limit", 50, "maximum rows")
		_ = sub.Parse(rest)
		q := fmt.Sprintf("/admin/conversions?since=%s&limit=%d", url.QueryEscape(*since), *limit)
		if *user != "" {
			q += "&user=" + url.QueryEscape(*user)
		}
		out := c.do("GET", q, nil)
		rows, _ := out["conversions"].([]any)
		if len(rows) == 0 {
			fmt.Println("no conversions in that window")
			break
		}
		fmt.Printf("%-20s %-26s %-8s %-6s %-6s %-9s %s\n",
			"WHEN", "USER", "KIND", "FMT", "OK", "SIZE", "TOOK")
		for _, r := range rows {
			m := r.(map[string]any)
			ok := "yes"
			if m["ok"] != true {
				ok = "FAIL"
			}
			when, _ := m["createdAt"].(string)
			when = strings.Replace(strings.TrimSuffix(when, "Z"), "T", " ", 1)
			bytesN, _ := m["bytes"].(float64)
			ms, _ := m["durationMs"].(float64)
			fmt.Printf("%-20s %-26v %-8v %-6v %-6s %-9s %s\n",
				when, m["userEmail"], m["kind"], m["format"], ok,
				humanBytes(int64(bytesN)), humanMS(int64(ms)))
		}

	case "requests list":
		sub := flag.NewFlagSet("requests list", flag.ExitOnError)
		all := sub.Bool("all", false, "include handled requests")
		_ = sub.Parse(rest)
		path := "/admin/requests"
		if *all {
			path += "?all=true"
		}
		out := c.do("GET", path, nil)
		rows, _ := out["requests"].([]any)
		if len(rows) == 0 {
			fmt.Println("nobody waiting")
			break
		}
		fmt.Printf("%-5s %-34s %-12s %s\n", "ID", "EMAIL", "ASKED", "STATUS")
		for _, r := range rows {
			m := r.(map[string]any)
			asked, _ := m["createdAt"].(string)
			if len(asked) >= 10 {
				asked = asked[:10]
			}
			fmt.Printf("%-5v %-34v %-12s %v\n", m["id"], m["email"], asked, m["status"])
		}
		fmt.Printf("\nSend one with: tome admin requests invite <id|email>\n")

	case "requests invite":
		requireArg(rest, "request id or email")
		sub := flag.NewFlagSet("requests invite", flag.ExitOnError)
		ttl := sub.Duration("ttl", 168*time.Hour, "invite validity")
		_ = sub.Parse(rest[1:])
		out := c.do("POST", "/admin/requests/"+url.PathEscape(rest[0])+"/invite",
			map[string]any{"ttlHours": int(ttl.Hours())})
		if out["emailed"] == true {
			fmt.Printf("invited %v (code %v, expires %v)\n", out["email"], out["code"], out["expiresAt"])
			warnBase(out)
		} else {
			// The code exists, so print it: the operator can still pass it on
			// by hand, and the request stays pending for a retry.
			fmt.Printf("created code %v for %v but the email failed: %v\n",
				out["code"], out["email"], out["emailError"])
		}

	case "requests dismiss":
		requireArg(rest, "request id or email")
		out := c.do("POST", "/admin/requests/"+url.PathEscape(rest[0])+"/dismiss", nil)
		fmt.Printf("dismissed %v\n", out["email"])

	case "settings get":
		printSettings(c.do("GET", "/admin/settings", nil))

	case "settings set":
		sub := flag.NewFlagSet("settings set", flag.ExitOnError)
		apiKey := sub.String("resend-api-key", "", "Resend API key (re_...)")
		from := sub.String("resend-from", "", "verified sender address, e.g. tome@yourdomain.com")
		phKey := sub.String("posthog-api-key", "",
			"PostHog project API key (phc_...) — enables product analytics; empty string clears it")
		phHost := sub.String("posthog-host", "",
			"PostHog host, e.g. https://eu.i.posthog.com (default https://us.i.posthog.com)")
		_ = sub.Parse(rest)
		body := map[string]any{}
		if *apiKey != "" {
			body["resendApiKey"] = *apiKey
		}
		if *from != "" {
			body["resendFrom"] = *from
		}
		// Passed explicitly so that --posthog-api-key="" turns analytics off,
		// rather than being indistinguishable from not mentioning the flag.
		sub.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "posthog-api-key":
				body["posthogApiKey"] = *phKey
			case "posthog-host":
				body["posthogHost"] = *phHost
			}
		})
		if len(body) == 0 {
			fatal("nothing to set (use --resend-api-key / --resend-from / " +
				"--posthog-api-key / --posthog-host)")
		}
		printSettings(c.do("PUT", "/admin/settings", body))

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

// humanBytes and humanMS keep the conversions table scannable — exact byte
// counts and millisecond figures are noise when you are looking for the run
// that failed or the one that took thirty seconds.
func humanBytes(n int64) string {
	switch {
	case n <= 0:
		return "-"
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.0fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

func humanMS(ms int64) string {
	if ms <= 0 {
		return "-"
	}
	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// warnBase surfaces a server-side warning that the links just emailed are
// unreachable. Printed after the success line rather than instead of it: the
// invite really was sent, and the operator needs to know to resend.
func warnBase(out map[string]any) {
	if w, _ := out["warning"].(string); w != "" {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

// printSettings renders the settings blob returned by GET/PUT /admin/settings.
// API keys are reported as set/unset — the server never echoes their values.
func printSettings(out map[string]any) {
	fmt.Printf("resend from:     %v\n", out["resendFrom"])
	fmt.Printf("resend api key:  set=%v\n", out["resendApiKeySet"])
	host := out["posthogHost"]
	if host == nil || host == "" {
		host = "(default)"
	}
	fmt.Printf("posthog host:    %v\n", host)
	fmt.Printf("posthog api key: set=%v", out["posthogApiKeySet"])
	switch out["posthogSource"] {
	case "environment":
		// Without this line, `settings set --posthog-api-key` looks like it
		// silently failed when the environment is the thing in charge.
		fmt.Print("  — from TOME_POSTHOG_API_KEY (environment wins over stored settings)")
	case "settings":
		fmt.Print("  — from stored settings")
	default:
		fmt.Print("  — analytics off")
	}
	fmt.Println()
}
