package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"cyberstrike-ai/internal/config"
	"cyberstrike-ai/internal/logger"
	"cyberstrike-ai/internal/mcp"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/test-external-mcp/main.go <config.yaml>")
		os.Exit(1)
	}

	configPath := os.Args[1]
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.ExternalMCP.Servers == nil || len(cfg.ExternalMCP.Servers) == 0 {
		fmt.Println("No external MCP servers configured")
		os.Exit(0)
	}

	fmt.Printf("Found %d external MCP server(s)\n\n", len(cfg.ExternalMCP.Servers))

	// Create the logger
	log := logger.New("info", "stdout")

	// Create the external MCP manager
	manager := mcp.NewExternalMCPManager(log.Logger)
	manager.LoadConfigs(&cfg.ExternalMCP)

	// Show configuration
	fmt.Println("=== Configuration ===")
	for name, srv := range cfg.ExternalMCP.Servers {
		fmt.Printf("\n%s:\n", name)
		fmt.Printf("  Transport: %s\n", getTransport(srv))
		if srv.Command != "" {
			fmt.Printf("  Command: %s\n", srv.Command)
			fmt.Printf("  Args: %v\n", srv.Args)
		}
		if srv.URL != "" {
			fmt.Printf("  URL: %s\n", srv.URL)
		}
		fmt.Printf("  Description: %s\n", srv.Description)
		fmt.Printf("  Timeout: %d seconds\n", srv.Timeout)
		fmt.Printf("  ExternalMCPEnable: %v\n", srv.ExternalMCPEnable)
	}

	// Get statistics
	fmt.Println("\n=== Statistics ===")
	stats := manager.GetStats()
	fmt.Printf("total: %d\n", stats["total"])
	fmt.Printf("enabled: %d\n", stats["enabled"])
	fmt.Printf("disabled: %d\n", stats["disabled"])
	fmt.Printf("connected: %d\n", stats["connected"])

	// Test startup (only the enabled ones)
	fmt.Println("\n=== Test startup ===")
	for name, srv := range cfg.ExternalMCP.Servers {
		if srv.ExternalMCPEnable {
			fmt.Printf("\ntrying to start %s...\n", name)
			// Note: actual startup may fail because a real MCP server is required
			err := manager.StartClient(name)
			if err != nil {
				fmt.Printf("  startup failed (this is normal if there is no real MCP server): %v\n", err)
			} else {
				fmt.Printf("  started successfully\n")
				// Get the client status
				if client, exists := manager.GetClient(name); exists {
					fmt.Printf("  status: %s\n", client.GetStatus())
					fmt.Printf("  connected: %v\n", client.IsConnected())
				}
			}
		}
	}

	// Wait a moment
	time.Sleep(2 * time.Second)

	// Test fetching the tool list
	fmt.Println("\n=== Test fetching the tool list ===")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tools, err := manager.GetAllTools(ctx)
	if err != nil {
		fmt.Printf("failed to fetch the tool list: %v\n", err)
	} else {
		fmt.Printf("fetched %d tools\n", len(tools))
		for i, tool := range tools {
			if i < 5 { // only show the first 5
				fmt.Printf("  - %s: %s\n", tool.Name, tool.Description)
			}
		}
		if len(tools) > 5 {
			fmt.Printf("  ... and %d more tools\n", len(tools)-5)
		}
	}

	// Test stopping
	fmt.Println("\n=== Test stopping ===")
	for name := range cfg.ExternalMCP.Servers {
		fmt.Printf("\nstopping %s...\n", name)
		err := manager.StopClient(name)
		if err != nil {
			fmt.Printf("  stop failed: %v\n", err)
		} else {
			fmt.Printf("  stopped successfully\n")
		}
	}

	// Final statistics
	fmt.Println("\n=== Final statistics ===")
	stats = manager.GetStats()
	fmt.Printf("total: %d\n", stats["total"])
	fmt.Printf("enabled: %d\n", stats["enabled"])
	fmt.Printf("disabled: %d\n", stats["disabled"])
	fmt.Printf("connected: %d\n", stats["connected"])

	fmt.Println("\n=== Test completed ===")
}

func getTransport(srv config.ExternalMCPServerConfig) string {
	t := srv.GetTransportType()
	if t == "" {
		return "unknown"
	}
	return t
}

