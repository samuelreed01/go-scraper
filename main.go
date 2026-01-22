package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"time"
)

func scrapeHandler(w http.ResponseWriter, r *http.Request) {
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

	var req ScrapeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	result, err := Scrape(req.URL)
	if err != nil {
		http.Error(w, "Scraping failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func auditHandler(w http.ResponseWriter, r *http.Request) {
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

	var req AuditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	err := req.Validate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	taskId := time.Now().String()

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
	}

	result, err := Audit(req.URL, taskId, keywords, checks)
	if err != nil {
		http.Error(w, "Audit failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	log.Printf("Starting scraper server on port %s", port)
	http.HandleFunc("/scrape", scrapeHandler)
	http.HandleFunc("/audit", auditHandler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
