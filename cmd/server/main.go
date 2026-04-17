package main

import (
	"log"
	"os"
	"strconv"

	"orbyt-flow/internal/api"
	"orbyt-flow/internal/executor"
	mcpsrv "orbyt-flow/internal/mcp"
	"orbyt-flow/internal/store"
)

func main() {
	dataDir := getEnv("FLOWENGINE_DATA_DIR", "./data")
	port, _ := strconv.Atoi(getEnv("PORT", "8085"))

	s := store.NewFileStore(dataDir)
	ex := executor.NewExecutor(s)

	if os.Getenv("MCP_MODE") == "true" {
		userID := getEnv("MCP_USER_ID", "default")
		srv := mcpsrv.NewMCPServer(s, ex, dataDir, userID)
		log.Println("orbyt-flow starting in MCP mode, user:", userID)
		if err := srv.Start(); err != nil {
			log.Fatal(err)
		}
	} else {
		srv := api.NewServer(s, ex, dataDir, port)
		log.Printf("orbyt-flow starting on port %d", port)
		if err := srv.Start(); err != nil {
			log.Fatal(err)
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
