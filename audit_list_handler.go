package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"github.com/chromedp/chromedp"
)

// AuditRequest structure
type AuditListRequest struct {
	URLs     []string `json:"urls"`
	Keywords []string `json:"keywords"`
	Checks   *Checks  `json:"checks"`
}

func (r *AuditListRequest) Validate() error {
	if r.URLs == nil {
		return errors.New("url is required")
	}
	// if r.Keywords == nil {
	// 	return errors.New("keywords is required")
	// }
	if r.Checks == nil {
		return errors.New("checks is required")
	}
	return nil
}

func auditListHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	apiKey := query.Get("api_key")
	if apiKey != os.Getenv("API_KEY") {
		http.Error(w, "Invalid API key", http.StatusUnauthorized)
		return
	}

	var req AuditListRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	err := req.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var keywords []string = req.Keywords
	if keywords == nil {
		keywords = make([]string, 0)
	}

	var checks Checks
	if req.Checks == nil {
		checks = Checks{
			Lighthouse:  true,
			Headings:    true,
			Title:       true,
			Description: true,
			Keywords:    true,
			Images:      true,
			Links:       true,
			Security:    true,
		}
	} else {
		checks = *req.Checks
	}

	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.NoSandbox,
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("no-zygote", true),
		chromedp.Flag("disable-background-networking", true),
		chromedp.Flag("disable-default-apps", true),
		chromedp.Flag("disable-sync", true),
		chromedp.Flag("disable-translate", true),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.Flag("disable-remote-fonts", true),
		chromedp.Flag("disable-background-timer-throttling", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-backgrounding-occluded-windows", true),
		chromedp.Flag("disable-renderer-backgrounding", true),
		chromedp.Flag("disable-features", "BackForwardCache"),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	w.Header().Set("Content-Type", "text/plain")

	for _, url := range req.URLs {
		result := AuditPage(AuditPageParams{
			Ctx:      allocCtx,
			PageURL:  url,
			Keywords: keywords,
			Checks:   checks,
		})

		output, err := json.Marshal(result)
		if err != nil {
			http.Error(w, "Audit failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(output)
		w.Write([]byte("___separator___"))
	}
}
