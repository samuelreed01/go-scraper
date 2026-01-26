package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync"

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

	MAX_TABS := 2
	if os.Getenv("AUDIT_TABS") != "" {
		num, err := strconv.Atoi(os.Getenv("AUDIT_TABS"))
		if err == nil {
			MAX_TABS = num
		}
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

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
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

	var wg sync.WaitGroup

	dividedUrls := divideUrls(req.URLs, MAX_TABS)

	for _, urls := range dividedUrls {
		wg.Go(func() {
			for _, url := range urls {
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
				flusher.Flush()
			}
		})
	}

	wg.Wait()
	flusher.Flush()
}

func divideUrls(urls []string, n int) [][]string {
	base := len(urls) / n
	remainder := len(urls) % n
	output := make([][]string, n)
	startAt := 0

	for i := range n {
		count := base
		if i < remainder {
			count++
		}
		output[i] = urls[startAt : startAt+count]
		startAt += count
	}

	return output
}
