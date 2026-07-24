package main

import (
	"embed"
	"github.com/1344812937/go-web-quick-start/cmd"
)

//go:embed all:frontend/dist/**
var staticFS embed.FS

func main() {
	run := make(chan int)
	app := cmd.InitializeApp()
	app.Start(staticFS)
	<-run
}
