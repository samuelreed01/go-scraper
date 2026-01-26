package main

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

type Checks struct {
	Lighthouse  bool `json:"lighthouse"`
	Headings    bool `json:"headings"`
	Title       bool `json:"title"`
	Description bool `json:"description"`
	Keywords    bool `json:"keywords"`
	Images      bool `json:"images"`
	Links       bool `json:"links"`
	Security    bool `json:"security"`
}

// checkH1 validates H1 heading elements and returns any warnings
func checkH1(h1Texts []string, pageURL string) map[WarningType][]string {
	warnings := make(map[WarningType][]string)

	// Check if multiple H1s exist
	if len(h1Texts) > 1 {
		warnings[WarningH1Multiple] = []string{pageURL, fmt.Sprintf("%d", len(h1Texts))}
	}

	// Check if H1 is missing
	if len(h1Texts) == 0 {
		warnings[WarningH1Missing] = []string{pageURL}
		return warnings
	}

	// Check if H1 text is empty
	if slices.Contains(h1Texts, "") {
		warnings[WarningH1Missing] = []string{pageURL}
	}

	return warnings
}

// checkTitle validates the page title and returns any warnings
func checkTitle(title string, pageURL string) map[WarningType][]string {
	warnings := make(map[WarningType][]string)

	// Check if title is missing
	if title == "" {
		warnings[WarningTitleMissing] = []string{pageURL}
		return warnings
	}

	// Check if title is too short
	if len(title) < 30 {
		warnings[WarningTitleTooShort] = []string{pageURL, title}
		return warnings
	}

	// Check if title is too long
	if len(title) > 65 {
		warnings[WarningTitleTooLong] = []string{pageURL, title}
		return warnings
	}

	return warnings
}

// checkDescription validates the meta description and returns any warnings
func checkDescription(metaDesc string, pageURL string) map[WarningType][]string {
	warnings := make(map[WarningType][]string)

	// Check if description is missing
	if metaDesc == "" {
		warnings[WarningMetaDescriptionMissing] = []string{pageURL}
		return warnings
	}

	// Check if description is too short
	if len(metaDesc) < 30 {
		warnings[WarningMetaDescriptionTooShort] = []string{pageURL, metaDesc}
		return warnings
	}

	// Check if description is too long
	if len(metaDesc) > 165 {
		warnings[WarningMetaDescriptionTooLong] = []string{pageURL, metaDesc}
		return warnings
	}

	return warnings
}

// checkLinks validates links on the page and returns any warnings
func checkLinkProtocol(linkHrefs []string, pageURL string) map[WarningType][]string {
	warnings := make(map[WarningType][]string)

	// Collect all HTTP links (non-HTTPS)
	httpLinks := []string{}
	for _, href := range linkHrefs {
		parsedHref, err := url.Parse(href)
		if err != nil {
			continue
		}

		// Check if link uses HTTP instead of HTTPS
		if parsedHref.Scheme == "http" {
			httpLinks = append(httpLinks, href)
		}
	}

	// Add warning with all HTTP links found
	if len(httpLinks) > 0 {
		warnings[WarningHTTPSToHTTPLinks] = append([]string{pageURL}, httpLinks...)
	}

	return warnings
}

var compiled = make(map[string]*regexp.Regexp)

func getRegex(keyword string) (*regexp.Regexp, error) {
	if re, ok := compiled[keyword]; ok {
		return re, nil
	}

	pattern := `\b` + regexp.QuoteMeta(keyword) + `\b`
	re, err := regexp.Compile("(?i)" + pattern)
	if err != nil {
		return nil, err
	}

	compiled[keyword] = re
	return re, nil
}

var (
	linkMap   = make(map[string]bool)
	linkMapMu sync.RWMutex
)

func linkWorker(
	jobs <-chan string,
	results chan<- string,
) {
	for link := range jobs {
		linkMapMu.RLock()
		works, existsInMap := linkMap[link]
		linkMapMu.RUnlock()

		if !existsInMap {
			works = isLinkAlive(link)

			linkMapMu.Lock()
			linkMap[link] = works
			linkMapMu.Unlock()
		}

		if !works {
			results <- link
		}
	}
}

func checkBrokenLinks(pageURL string, links []string) map[WarningType][]string {
	warnings := make(map[WarningType][]string)

	jobs := make(chan string)
	results := make(chan string)

	var wg sync.WaitGroup

	// Spawn 5 workers
	for range 5 {
		wg.Go(func() {
			linkWorker(jobs, results)
		})
	}

	// Close results when workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Feed jobs
	go func() {
		for _, link := range links {
			jobs <- link
		}
		close(jobs)
	}()

	// Collect results
	for brokenLink := range results {
		if len(warnings[WarningLinksBroken]) == 0 {
			warnings[WarningLinksBroken] = []string{pageURL}
		}
		warnings[WarningLinksBroken] = append(warnings[WarningLinksBroken], brokenLink)
	}

	return warnings
}

func isLinkAlive(url string) bool {
	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Range", "bytes=0-0")
	if err != nil {
		return false
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Consider 2xx and 3xx as "alive"
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}

func checkKeywords(content string, keywords []string, keywordMap map[string]int) {
	for _, keywordPhrase := range keywords {
		keywordArray := strings.Fields(keywordPhrase)

		matchExists := true
		for _, keyword := range keywordArray {
			re, err := getRegex(keyword)
			if err != nil || !re.MatchString(content) {
				matchExists = false
				break
			}
		}

		if matchExists {
			keywordMap[keywordPhrase]++
		}
	}
}
