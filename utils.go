package main

import (
	"fmt"
	"time"

	"resty.dev/v3"
)

// Cache settings are initialized from AppConfig before the database is opened.
var (
	CacheExpire = 24 * time.Hour
	DbPath      = "./data/database.db"
)

func FetchString(url string) (string, error) {
	L().Info("Fetching remote resource")

	client := resty.New().SetRetryCount(3)
	defer func() { _ = client.Close() }()
	res, err := client.R().Get(url)

	if err != nil {
		return "", fmt.Errorf("remote resource request failed")
	}
	if res.StatusCode() < 200 || res.StatusCode() >= 300 {
		return "", fmt.Errorf("remote resource returned HTTP %d", res.StatusCode())
	}

	return res.String(), nil
}
