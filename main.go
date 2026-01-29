package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "5000"
	}
	log.Printf("Starting scraper server on port %s", port)
	http.HandleFunc("/scrape", scrapeSiteHandler)
	http.HandleFunc("/audit", auditListHandler)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}
