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

type ScrapeResponse struct {
	Results []ScrapeResult `json:"results"`
}

// AuditRequest structure
type ScrapeRequest struct {
	URLs []string `json:"urls"`
}

func (r *ScrapeRequest) Validate() error {
	if len(r.URLs) == 0 {
		return errors.New("no target urls provided")
	}
	return nil
}

func scrapeSiteHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	MAX_TABS := 1
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

	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	err := req.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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

	w.Header().Set("Content-Type", "application/json")

	resultsChannel := make(chan ScrapeResult)
	var wg sync.WaitGroup

	dividedUrls := divideUrls(req.URLs, MAX_TABS)

	for _, urls := range dividedUrls {
		wg.Go(func() {
			for _, url := range urls {
				select {
				case <-r.Context().Done():
					return
				default:
				}

				result, err := Scrape(url, allocCtx)
				if err == nil {
					resultsChannel <- *result
				}
			}
		})
	}

	output := make([]ScrapeResult, 0, len(req.URLs))

	go func() {
		wg.Wait()
		close(resultsChannel)
	}()

	for result := range resultsChannel {
		output = append(output, result)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	jsonErr := json.NewEncoder(w).Encode(ScrapeResponse{Results: output})
	if jsonErr != nil {
		http.Error(w, jsonErr.Error(), http.StatusInternalServerError)
		return
	}
}
