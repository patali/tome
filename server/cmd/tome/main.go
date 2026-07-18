// Command tome runs the local server that turns extracted X articles into
// Kindle-friendly EPUBs and (optionally) emails them via Send-to-Kindle.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/patali/tome/server/internal/article"
	"github.com/patali/tome/server/internal/epubgen"
	"github.com/patali/tome/server/internal/kindle"
	"github.com/patali/tome/server/internal/pdfgen"
)

// defaultFormat prefers PDF (faithful, images load) when Chrome is available.
func defaultFormat() string {
	if pdfgen.Available() {
		return "pdf"
	}
	return "epub"
}

// buildDoc renders the article to the requested format ("pdf" | "epub" | "").
func buildDoc(a article.Article, format string) (data []byte, filename, contentType string, err error) {
	if format == "" {
		format = defaultFormat()
	}
	switch format {
	case "pdf":
		data, filename, err = pdfgen.Build(a)
		return data, filename, "application/pdf", err
	case "epub":
		data, filename, err = epubgen.Build(a)
		return data, filename, "application/epub+zip", err
	default:
		return nil, "", "", fmt.Errorf("unknown format %q (use pdf or epub)", format)
	}
}

// deliveryMethod reports how /send-to-kindle would deliver right now.
func deliveryMethod(cfg kindle.Config) string {
	if cfg.SMTPReady() {
		return "smtp"
	}
	if runtime.GOOS == "darwin" && cfg.KindleEmail != "" {
		return "mail-app"
	}
	return "none"
}

const maxBody = 8 << 20 // 8 MiB

func main() {
	addr := ":8080"
	if p := os.Getenv("TOME_PORT"); p != "" {
		addr = ":" + p
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/status", handleStatus)
	mux.HandleFunc("/convert", handleConvert)
	mux.HandleFunc("/send-to-kindle", handleSendToKindle)

	log.Printf("tome server listening on http://localhost%s", addr)
	kcfg := kindle.LoadConfig()
	switch deliveryMethod(kcfg) {
	case "smtp":
		log.Printf("send-to-kindle -> %s via SMTP (%s)", kcfg.KindleEmail, kcfg.SMTPHost)
	case "mail-app":
		log.Printf("send-to-kindle -> %s via macOS Mail.app (SMTP not set: %v)", kcfg.KindleEmail, kcfg.MissingSMTP())
	default:
		log.Printf("send-to-kindle disabled (no Kindle email / not on macOS); /convert still works")
	}
	log.Fatal(http.ListenAndServe(addr, cors(mux)))
}

// cors allows the browser extension (a chrome-extension:// origin) to call in.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func handleStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := kindle.LoadConfig()
	method := deliveryMethod(cfg)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"service":          "tome",
		"kindleConfigured": method != "none",
		"method":           method, // "smtp" | "mail-app" | "none"
		"kindleEmail":      cfg.KindleEmail,
		"defaultFormat":    defaultFormat(),
		"pdfAvailable":     pdfgen.Available(),
	})
}

// decodeArticle reads and validates the JSON body.
func decodeArticle(w http.ResponseWriter, r *http.Request) (article.Article, bool) {
	var a article.Article
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST required"})
		return a, false
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
		return a, false
	}
	if err := json.Unmarshal(body, &a); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON: " + err.Error()})
		return a, false
	}
	if a.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "article content is empty"})
		return a, false
	}
	return a, true
}

// handleConvert returns the rendered document as a download (no email).
// Format: ?format=pdf|epub, else the article's format field, else the default.
func handleConvert(w http.ResponseWriter, r *http.Request) {
	a, ok := decodeArticle(w, r)
	if !ok {
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" {
		format = a.Format
	}
	data, filename, contentType, err := buildDoc(a, format)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// handleSendToKindle builds the EPUB and emails it to the configured Kindle.
func handleSendToKindle(w http.ResponseWriter, r *http.Request) {
	a, ok := decodeArticle(w, r)
	if !ok {
		return
	}
	data, filename, _, err := buildDoc(a, a.Format)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	cfg := kindle.LoadConfig()

	switch deliveryMethod(cfg) {
	case "smtp":
		if err := kindle.Send(cfg, filename, data, a.Title); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "method": "smtp", "sentTo": cfg.KindleEmail,
			"filename": filename, "bytes": len(data),
		})

	case "mail-app":
		// Mail attaches by reference, so persist the EPUB (don't delete it).
		dir := filepath.Join(os.TempDir(), "tome")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, data, 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := kindle.ComposeInMail(cfg.KindleEmail, a.Title, path); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "method": "mail-app", "sentTo": cfg.KindleEmail,
			"filename": filename, "bytes": len(data), "attachment": path,
		})

	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "no delivery method: configure SMTP (TOME_*) or run on macOS with a Kindle email set",
		})
	}
}
