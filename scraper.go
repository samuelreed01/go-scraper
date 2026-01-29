package main

import (
	"context"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// Response structure
type ScrapeResult struct {
	Text       string `json:"text"`
	Images     int    `json:"images"`
	Heading    int    `json:"headings"`
	Paragraphs int    `json:"paragraphs"`
	Words      int    `json:"words"`
}

func Scrape(url string, parentCtx context.Context) (*ScrapeResult, error) {
	// Context with timeout for this specific page
	ctx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()

	// Create a new browser context from the shared allocator
	taskCtx, taskCancel := chromedp.NewContext(ctx)
	defer taskCancel()

	var pageText string
	var imgCount int
	var paragraphCount int
	var headingsCount int

	err := chromedp.Run(taskCtx,
		chromedp.Navigate(url),
		// chromedp.ActionFunc(func(ctx context.Context) error {
		// 	startup = time.Since(startTime)
		// 	return nil
		// }),
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
		Text:       pageText,
		Images:     imgCount,
		Heading:    headingsCount,
		Paragraphs: paragraphCount,
		Words:      wordCount,
	}, nil
}
