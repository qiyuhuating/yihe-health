package main

import (
	"encoding/json"
	"net/http"
	"os"
	"time"
)

func main() {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8080/api/v1/service/status")
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()
	var status struct {
		Ready bool `json:"databaseReady"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(resp.Body).Decode(&status) != nil || !status.Ready {
		os.Exit(1)
	}
}
