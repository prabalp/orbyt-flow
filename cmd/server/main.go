package main

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"

	"orbyt-flow/internal/api"
	"orbyt-flow/internal/executor"
	mcpsrv "orbyt-flow/internal/mcp"
	"orbyt-flow/internal/services"
	"orbyt-flow/internal/store"
)

func main() {
	// Load .env from cwd if present; ignore missing file.
	_ = godotenv.Load()
	if p := os.Getenv("ORBYT_DOTENV"); p != "" {
		_ = godotenv.Load(p)
	}

	dataDir := getEnv("FLOWENGINE_DATA_DIR", "./data")
	port, _ := strconv.Atoi(getEnv("PORT", "8085"))

	services.SetDataDir(dataDir)

	s := store.NewFileStore(dataDir)
	ex := executor.NewExecutor(s)
	ex.DataDir = dataDir

	if os.Getenv("MCP_MODE") == "true" {
		userID := getEnv("MCP_USER_ID", "default")
		srv := mcpsrv.NewMCPServer(s, ex, dataDir, userID)
		log.Println("orbyt-flow starting in MCP mode, user:", userID)
		if err := srv.Start(); err != nil {
			log.Fatal(err)
		}
	} else {
		srv := api.NewServer(s, ex, dataDir, port)
		srv.SetAdminPassword(getEnv("ORBYT_ADMIN_PASSWORD", ""))
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
