package main

import (
	"embed"
	"log"

	"github.com/1344812937/go-web-quick-start/cmd"
	"github.com/1344812937/go-web-quick-start/internal/config"
)

//go:embed all:frontend/dist/**
var staticFS embed.FS

func main() {
	if err := config.LoadRuntimeEnv(); err != nil {
		log.Fatalf("load runtime environment: %v", err)
	}
	run := make(chan int)
	app := cmd.InitializeApp()
	app.Start(staticFS)
	<-run
}
