package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/chromedp/chromedp"
)

// Response structure
type ScrapeResult struct {
	Text  string   `json:"text"`
	Images []map[string]string `json:"images"`
}

// Request structure
type ScrapeRequest struct {
	URL string `json:"url"`
}

func scrape(url string) (*ScrapeResult, error) {
	// Context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Chromium options suitable for containers
	opts := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Headless,
		chromedp.DisableGPU,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("mute-audio", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(ctx, opts...)
	defer allocCancel()

	taskCtx, taskCancel := chromedp.NewContext(allocCtx)
	defer taskCancel()

	var pageText string
	var imgSrcs []map[string]string

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Text("body", &pageText, chromedp.NodeVisible, chromedp.ByQuery),
		chromedp.AttributesAll("img", &imgSrcs, chromedp.ByQueryAll),
	)
	if err != nil {
		return nil, err
	}

	return &ScrapeResult{
		Text:  pageText,
		Images: imgSrcs,
	}, nil
}

func handler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	query := r.URL.Query()
	apiKey := query.Get("api_key")
	if (apiKey != os.Getenv("API_KEY")) {
		http.Error(w, "Invalid API key", http.StatusUnauthorized)
		return
	}

	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := scrape(req.URL)
	if err != nil {
		http.Error(w, "Scraping failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func main() {
	port := "5000"
	log.Printf("Starting scraper server on port %s", port)
	http.HandleFunc("/scrape", handler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}