package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
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

// AuditPageResult combines page info and discovered links
type AuditPageResult struct {
	Warnings WarningMap
	Url      string
	Links    []string
	H1Texts  []string
	Title    string
	Error    string
}

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
	parsedURL, err := url.Parse(startURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	baseHost := parsedURL.Host

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
		chromedp.Flag("renderer-process-limit", 2),
		chromedp.Flag("process-per-site", true),
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
	keywordChannel := make(chan map[string]string, 100)

	// Define task function that audits a page using the shared allocator
	taskFunc := func(pageURL string) (AuditPageResult, error) {
		result := auditPage(AuditPageParams{
			Ctx:            allocCtx,
			PageURL:        pageURL,
			BaseHost:       baseHost,
			Keywords:       keywords,
			KeywordChannel: keywordChannel,
			Checks:         checks,
		})
		pagesSoFar++
		pubSubClient.Publish(
			PubSubMessage{
				TaskID: taskId,
				Event:  "page",
				Message: map[string]any{
					"url":      pageURL,
					"warnings": result.Warnings,
					"total":    pagesSoFar,
				},
			})
		return result, nil
	}

	// Start the worker pool
	pool.Start(taskFunc)

	pubSubClient.Subscribe(taskId, func(data PubSubMessage) {
		if data.Event == "cancel" {
			// cancel whole audit
			pool.Stop()
		}
	})

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

func getFileExtension(urlToVisit string) string {
	u, err := url.Parse(urlToVisit)
	if err != nil {
		return ""
	}

	parts := strings.Split(u.Path, ".")

	var fileExtension string
	if len(parts) > 1 {
		fileExtension = parts[len(parts)-1]
	} else {
		fileExtension = ""
	}

	return fileExtension
}

var pageExtensions = map[string]bool{
	"html": true,
	"htm":  true,
	"xml":  true,
	"aspx": true,
	"php":  true,
	"asp":  true,
	"jsp":  true,
}

type AuditPageParams struct {
	Ctx            context.Context
	PageURL        string
	BaseHost       string
	Keywords       []string
	KeywordChannel chan map[string]string
	Checks         Checks
}

// auditPage audits a single page and returns its info and same-host links
func auditPage(p AuditPageParams) AuditPageResult {
	fileExt := getFileExtension(p.PageURL)

	if fileExt != "" && !pageExtensions[fileExt] {
		return AuditPageResult{
			Url: p.PageURL,
		}
	}

	// Context with timeout for this specific page
	ctx, cancel := context.WithTimeout(p.Ctx, 30*time.Second)
	defer cancel()

	// Create a new browser context from the shared allocator
	taskCtx, taskCancel := chromedp.NewContext(ctx)
	defer taskCancel()

	var title string
	var pageText string
	var metaDesc string
	var linkHrefs []string
	var h1Texts []string

	err := chromedp.Run(taskCtx,
		network.Enable(),
		network.SetBlockedURLs([]string{
			"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp",
			"*.svg", "*.woff", "*.woff2", "*.ttf", "*.otf",
			"*.mp4", "*.webm",
		}),

		chromedp.Navigate(p.PageURL),
		chromedp.WaitVisible("body", chromedp.ByQuery),

		chromedp.Text("body", &pageText, chromedp.NodeVisible, chromedp.ByQuery),

		// Get title
		chromedp.Title(&title),

		// Get meta description
		chromedp.EvaluateAsDevTools(`
			(document.querySelector('meta[name="description"]') || {}).content || ""
		`, &metaDesc),

		// Get H1 texts
		chromedp.EvaluateAsDevTools(`
			Array.from(document.querySelectorAll("h1"))
			     .map(el => el.innerText.trim())
		`, &h1Texts),

		// Get all link hrefs
		chromedp.EvaluateAsDevTools(`
			Array.from(document.querySelectorAll("a[href]"))
			     .map(el => el.href)
		`, &linkHrefs),
	)

	if err != nil {
		return AuditPageResult{
			Url:      p.PageURL,
			Error:    err.Error(),
			Warnings: WarningMap{},
			Links:    []string{},
			H1Texts:  []string{},
		}
	}

	// Run all validation checks and collect warnings
	allWarnings := make(WarningMap)

	// Merge warnings from each check
	if p.Checks.Headings {
		mergeWarnings(allWarnings, checkH1(h1Texts, p.PageURL))
	}
	if p.Checks.Title {
		mergeWarnings(allWarnings, checkTitle(title, p.PageURL))
	}
	if p.Checks.Description {
		mergeWarnings(allWarnings, checkDescription(metaDesc, p.PageURL))
	}
	if p.Checks.Links {
		mergeWarnings(allWarnings, checkBrokenLinks(p.PageURL, linkHrefs))
	}
	if p.Checks.Security {
		mergeWarnings(allWarnings, checkLinkProtocol(linkHrefs, p.PageURL))
	}
	if p.Checks.Keywords && len(p.Keywords) > 0 {
		checkKeywords(p.PageURL, title+" "+pageText, p.Keywords, p.KeywordChannel)
	}

	// Filter links to only include same-host URLs
	sameHostLinks := []string{}
	for _, href := range linkHrefs {
		parsedHref, err := url.Parse(href)
		if err != nil {
			continue
		}

		// Only include links with the same host
		if parsedHref.Host == p.BaseHost {
			sameHostLinks = append(sameHostLinks, href)
		}
	}

	return AuditPageResult{
		Url:      p.PageURL,
		Title:    title,
		Warnings: allWarnings,
		Links:    sameHostLinks,
		H1Texts:  h1Texts,
	}
}

func mergeWarnings(allWarnings WarningMap, pageWarnings map[WarningType][]string) {
	for warningType, warnings := range pageWarnings {
		allWarnings[warningType] = append(allWarnings[warningType], warnings)
	}
}
