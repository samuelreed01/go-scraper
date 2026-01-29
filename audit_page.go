package main

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

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
	Ctx          context.Context
	PageURL      string
	Keywords     []string
	Checks       Checks
	CheckedPaths []string
}

// AuditPageResult combines page info and discovered links
type AuditPageResult struct {
	Warnings       WarningMap     `json:"warnings"`
	Url            string         `json:"url"`
	Links          []string       `json:"links"`
	H1Texts        []string       `json:"h1s"`
	Title          string         `json:"title"`
	Error          string         `json:"error"`
	KeywordMatches map[string]int `json:"keywordMatches"`
}

// auditPage audits a single page and returns its info and same-host links
func AuditPage(p AuditPageParams) AuditPageResult {
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
	h1Texts := make([]string, 2)
	keywordMatches := make(map[string]int)

	err := chromedp.Run(taskCtx,
		network.Enable(),
		network.SetBlockedURLs([]string{
			"*.png", "*.jpg", "*.jpeg", "*.gif", "*.webp",
			"*.svg", "*.woff", "*.woff2", "*.ttf", "*.otf",
			"*.mp4", "*.webm",
		}),

		chromedp.Navigate(p.PageURL),
		chromedp.WaitVisible("a[href]", chromedp.ByQuery),

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
			Url:            p.PageURL,
			Error:          err.Error(),
			Warnings:       WarningMap{},
			Links:          []string{},
			H1Texts:        []string{},
			KeywordMatches: keywordMatches,
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
		checkedPathsMap := make(map[string]bool)
		if p.CheckedPaths != nil {
			for _, checkedPath := range p.CheckedPaths {
				checkedPathsMap[checkedPath] = true
			}
		}

		mergeWarnings(allWarnings, checkBrokenLinks(p.PageURL, linkHrefs, checkedPathsMap))
	}
	if p.Checks.Security {
		mergeWarnings(allWarnings, checkLinkProtocol(linkHrefs, p.PageURL))
	}
	if p.Checks.Keywords && len(p.Keywords) > 0 {
		checkKeywords(title+" "+pageText, p.Keywords, keywordMatches)
	}

	// Filter links to only include same-host URLs
	sameHostLinks := []string{}
	parsedBase, _ := url.Parse(p.PageURL)
	for _, href := range linkHrefs {
		parsedHref, err := url.Parse(href)
		if err != nil {
			continue
		}

		// Only include links with the same host
		if parsedHref.Host == parsedBase.Host {
			sameHostLinks = append(sameHostLinks, href)
		}
	}

	return AuditPageResult{
		Url:            p.PageURL,
		Title:          title,
		Warnings:       allWarnings,
		Links:          sameHostLinks,
		H1Texts:        h1Texts,
		KeywordMatches: keywordMatches,
	}
}

func mergeWarnings(allWarnings WarningMap, pageWarnings map[WarningType][]string) {
	for warningType, warnings := range pageWarnings {
		allWarnings[warningType] = append(allWarnings[warningType], warnings)
	}
}
