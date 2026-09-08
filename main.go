package main

import (
	"flag"
	"log"
	"os"
	"time"
)

func main() {
	configPath := flag.String("config", "", "Path to server YAML configuration (defaults to ./config.yaml when present)")
	flag.Parse()
	if *configPath == "" {
		if _, err := os.Stat("config.yaml"); err == nil {
			*configPath = "config.yaml"
		} else if !os.IsNotExist(err) {
			log.Fatal("cannot access config.yaml")
		}
	}
	cfg, err := loadAppConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	DbPath = cfg.Server.DBPath
	CacheExpire = time.Duration(cfg.Server.CacheExpireSec) * time.Second
	InitDb()
	ginEngine := setupRouter(cfg)
	err = ginEngine.Run(cfg.Server.Listen)
	if err != nil {
		log.Fatal(err)
	}
}
