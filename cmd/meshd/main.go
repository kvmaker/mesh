package main

import (
	"os"

	"github.com/maxyu/mesh/internal/server"
)

func main() {
	if err := server.NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
