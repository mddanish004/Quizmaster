package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// healthcheckMain is used by Docker HEALTHCHECK: /quizforge healthcheck --url=...
func healthcheckMain() {
	target := "http://127.0.0.1:8080/health/live"
	for _, a := range os.Args[2:] {
		var v string
		if _, err := fmt.Sscanf(a, "--url=%s", &v); err == nil && v != "" {
			target = v
		}
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "healthcheck bad status:", resp.Status)
		os.Exit(1)
	}
}
