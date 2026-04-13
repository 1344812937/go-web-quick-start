package main

import (
	"embed"
	"tank-tool/cmd"
)

//go:embed all:frontend/dist/**
var staticFS embed.FS

func main() {
	run := make(chan int)
	app := cmd.InitializeApp()
	app.Start(staticFS)
	<-run
}
