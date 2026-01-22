package main

import (
	"context"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// Response structure
type ScrapeResult struct {
	Text  string   `json:"text"`
	Images int `json:"images"`
	Heading int `json:"headings"`
	Paragraphs int `json:"paragraphs"`
	Words int `json:"words"`
}

// Request structure
type ScrapeRequest struct {
	URL string `json:"url"`
}

func Scrape(url string) (*ScrapeResult, error) {
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
	var imgCount int
	var paragraphCount int
	var headingsCount int

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQuery),
		chromedp.Text("body", &pageText, chromedp.NodeVisible, chromedp.ByQuery),
		chromedp.EvaluateAsDevTools(`
			document.querySelectorAll("h1,h2,h3,h4,h5,h6").length
		`, &headingsCount),
		chromedp.EvaluateAsDevTools(`
			document.querySelectorAll("img").length
		`, &imgCount),
		chromedp.EvaluateAsDevTools(`
			document.querySelectorAll("p").length
		`, &paragraphCount),
	)
	if err != nil {
		return nil, err
	}

	wordCount := len(strings.Fields(pageText))

	return &ScrapeResult{
		Text:  pageText,
		Images: imgCount,
		Heading: headingsCount,
		Paragraphs: paragraphCount,
		Words: wordCount,
	}, nil
}