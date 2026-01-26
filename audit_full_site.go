package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/chromedp/chromedp"
)

// WarningType represents the type of SEO/accessibility warning
type WarningType string

const (
	WarningH1Missing               WarningType = "h1_missing"
	WarningH1Multiple              WarningType = "h1_multiple"
	WarningH1Duplicate             WarningType = "h1_duplicate"
	WarningTitleMissing            WarningType = "title_missing"
	WarningTitleMultiple           WarningType = "title_multiple"
	WarningTitleDuplicate          WarningType = "title_duplicate"
	WarningTitleTooShort           WarningType = "title_too_short"
	WarningTitleTooLong            WarningType = "title_too_long"
	WarningMetaDescriptionMissing  WarningType = "meta_description_missing"
	WarningMetaDescriptionMultiple WarningType = "meta_description_multiple"
	WarningMetaDescriptionTooShort WarningType = "meta_description_too_short"
	WarningMetaDescriptionTooLong  WarningType = "meta_description_too_long"
	WarningImageSizeTooBig         WarningType = "image_size_too_big"
	WarningImageURLBroken          WarningType = "image_url_broken"
	WarningLinksBroken             WarningType = "links_broken"
	WarningSSLNo                   WarningType = "ssl_no"
	WarningHTTPSToHTTPLinks        WarningType = "https_to_http_links"
	WarningTimeoutPageLoad         WarningType = "timeout_page_load"
	WarningKeywordsMissing         WarningType = "keywords_missing"
)

const MaxAuditPages = 20

// AuditResult contains information about all audited pages
type AuditResult struct {
	Pages    []string   `json:"pages"`
	Warnings WarningMap `json:"warnings"`
}

// example: {"h1_missing": [["https://example.com"], ["https://example2.com"]], "title_too_long": [["https://example.com", "very long title"]]}
type WarningMap = map[WarningType][][]string

// PageAuditInfo contains audit information for a single page
type PageAuditInfo struct {
	URL        string     `json:"url"`
	StatusCode int        `json:"status_code"`
	Title      string     `json:"title"`
	Warnings   WarningMap `json:"warnings,omitempty"`
	Error      string     `json:"error,omitempty"`
}

// return type AuditResult = {
//   url: string;
//   links: string[];
//   warnings: any;
//   h1s: string[];
//   titles: string[];
//   keywordMatches: Record<string, number>;
// };

// AuditRequest structure
type AuditRequest struct {
	URL      string   `json:"url"`
	Keywords []string `json:"keywords"`
	Checks   *Checks  `json:"checks"`
}

func (r *AuditRequest) Validate() error {
	if r.URL == "" {
		return errors.New("url is required")
	}
	// if r.Keywords == nil {
	// 	return errors.New("keywords is required")
	// }
	// if r.Checks == nil {
	// 	return errors.New("checks is required")
	// }
	return nil
}

// Audit crawls a website starting from the given URL, following same-host links
func Audit(startURL string, taskId string, keywords []string, checks Checks) (*AuditResult, error) {
	// Parse the starting URL to get the host
	_, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	pubSubClient, err := NewPubSubClient(context.Background())
	if err != nil {
		return nil, err
	}
	defer pubSubClient.Close()

	// Create a single Chrome instance (ExecAllocator) shared by all workers
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

	var WORKERS int
	num, err := strconv.Atoi(os.Getenv("CHROME_WORKERS"))
	if err != nil {
		WORKERS = 5
	} else {
		WORKERS = num
	}

	// Create worker pool with 10 concurrent workers
	pool := NewWorkerPool[AuditPageResult](WORKERS)

	pagesSoFar := 0

	// Define task function that audits a page using the shared allocator
	taskFunc := func(pageURL string) (AuditPageResult, error) {
		result := AuditPage(AuditPageParams{
			Ctx:      allocCtx,
			PageURL:  pageURL,
			Keywords: keywords,
			Checks:   checks,
		})
		pagesSoFar++
		return result, nil
	}

	// Start the worker pool
	pool.Start(taskFunc)

	unsubscribe, err := pubSubClient.Subscribe(taskId, func(data PubSubMessage) {
		if data.Event == "cancel" {
			// cancel whole audit
			pool.Stop()
		}
	})
	defer unsubscribe()

	// Add the starting URL
	pool.AddTask(startURL)

	// Process results as they come in, adding new links to the pool
	// Keep checking until we've processed MaxAuditPages or no more tasks
	for {
		results := pool.GetResults()

		// Check if we've reached the limit
		if len(results) >= MaxAuditPages {
			break
		}

		// Add new links from completed results
		hasNewLinks := false
		for _, taskResult := range results {
			for _, link := range taskResult.Result.Links {
				// AddTask returns true if the task was added (not a duplicate)
				if pool.AddTask(link) {
					hasNewLinks = true
					// Stop adding if we've reached the limit
					if pool.HasBeenProcessed(link) && len(pool.processed) >= MaxAuditPages {
						break
					}
				}
			}
		}

		// If no new links were added and we have results, we're done
		if !hasNewLinks && len(results) > 0 {
			// Give workers a moment to finish any pending tasks
			time.Sleep(100 * time.Millisecond)
			finalResults := pool.GetResults()
			if len(finalResults) == len(results) {
				break
			}
		}

		// Brief sleep to avoid busy-waiting
		time.Sleep(50 * time.Millisecond)
	}

	// Stop the pool and get final results
	pool.Stop()
	taskResults := pool.GetResults()

	// Create maps to track H1s and titles across all pages
	h1Map := make(map[string][]string)
	titleMap := make(map[string][]string)

	// Convert TaskResults to PageAuditInfo and collect H1s/titles
	pages := make([]PageAuditInfo, 0, len(taskResults))
	for _, taskResult := range taskResults {
		auditResult := taskResult.Result

		// Create PageAuditInfo from AuditPageResult
		pageInfo := PageAuditInfo{
			URL:      auditResult.Url,
			Title:    auditResult.Title,
			Warnings: auditResult.Warnings,
			Error:    auditResult.Error,
		}
		pages = append(pages, pageInfo)

		// Collect H1 texts for duplicate detection
		for _, h1Text := range auditResult.H1Texts {
			if h1Text != "" {
				h1Map[h1Text] = append(h1Map[h1Text], auditResult.Url)
			}
		}

		// Collect title for duplicate detection
		if auditResult.Title != "" {
			titleMap[auditResult.Title] = append(titleMap[auditResult.Title], auditResult.Url)
		}

		// Limit to MaxAuditPages
		if len(pages) >= MaxAuditPages {
			break
		}
	}

	pageUrls := make([]string, 0, len(pages))
	allWarnings := make(map[WarningType][][]string)

	for _, page := range pages {
		pageUrls = append(pageUrls, page.URL)
		for warningType, warnings := range page.Warnings {
			allWarnings[warningType] = append(allWarnings[warningType], warnings...)
		}
	}

	// warnings := make(WarningMap)
	// h1Warnings := make([]string, 0)
	// titleWarnings := make([]string, 0)

	// for h1Text, urls := range h1Map {
	// 	if (len(urls) > 1) {
	// 		h1Warnings = append(h1Warnings, h1Text)
	// 	}
	// }

	return &AuditResult{
		Pages:    pageUrls,
		Warnings: allWarnings,
	}, nil
}
