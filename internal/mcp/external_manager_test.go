package mcp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cyberstrike-ai/internal/authctx"
	"cyberstrike-ai/internal/config"

	"go.uber.org/zap"
)

func TestExternalManagerEnforcesConfiguredAuthorizer(t *testing.T) {
	manager := NewExternalMCPManager(zap.NewNop())
	t.Cleanup(manager.StopAll)
	manager.SetToolAuthorizer(func(context.Context, string, map[string]interface{}) error {
		return errors.New("denied by policy")
	})
	ctx := authctx.WithPrincipal(context.Background(), authctx.NewPrincipal("u1", "user", "assigned", map[string]bool{"agent:execute": true}))
	_, executionID, err := manager.CallTool(ctx, "server::tool", map[string]interface{}{})
	if err == nil || !strings.Contains(err.Error(), "authorization denied") {
		t.Fatalf("external call bypassed authorizer: %v", err)
	}
	if executionID == "" {
		t.Fatal("denied external call should still return an execution id")
	}
	execution, ok := manager.GetExecution(executionID)
	if !ok || execution == nil {
		t.Fatalf("missing denied external execution %q", executionID)
	}
	if execution.Status != ToolExecutionStatusFailed || !strings.Contains(execution.Error, "denied by policy") {
		t.Fatalf("denied external execution = %#v, want failed with policy error", execution)
	}
}

func TestExternalMCPManager_AddOrUpdateConfig(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	// Test adding a stdio configuration
	stdioCfg := config.ExternalMCPServerConfig{
		Command:           "python3",
		Args:              []string{"/path/to/script.py"},
		Description:       "Test stdio MCP",
		Timeout:           30,
		ExternalMCPEnable: true,
	}

	err := manager.AddOrUpdateConfig("test-stdio", stdioCfg)
	if err != nil {
		t.Fatalf("failed to add stdio configuration: %v", err)
	}

	// Test adding an HTTP configuration
	httpCfg := config.ExternalMCPServerConfig{
		Type:              "http",
		URL:               "http://127.0.0.1:8081/mcp",
		Description:       "Test HTTP MCP",
		Timeout:           30,
		ExternalMCPEnable: false,
	}

	err = manager.AddOrUpdateConfig("test-http", httpCfg)
	if err != nil {
		t.Fatalf("failed to add HTTP configuration: %v", err)
	}

	// Verify the configuration was saved
	configs := manager.GetConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configurations, got %d", len(configs))
	}

	if configs["test-stdio"].Command != stdioCfg.Command {
		t.Errorf("stdio configuration command does not match")
	}

	if configs["test-http"].URL != httpCfg.URL {
		t.Errorf("HTTP configuration URL does not match")
	}
}

func TestExternalMCPManager_RemoveConfig(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	cfg := config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: false,
	}

	manager.AddOrUpdateConfig("test-remove", cfg)

	// Remove the configuration
	err := manager.RemoveConfig("test-remove")
	if err != nil {
		t.Fatalf("failed to remove configuration: %v", err)
	}

	configs := manager.GetConfigs()
	if _, exists := configs["test-remove"]; exists {
		t.Error("the configuration should have been removed")
	}
}

func TestExternalMCPManager_GetStats(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	// Add multiple configurations
	manager.AddOrUpdateConfig("enabled1", config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: true,
	})

	manager.AddOrUpdateConfig("enabled2", config.ExternalMCPServerConfig{
		URL:               "http://127.0.0.1:8081/mcp",
		ExternalMCPEnable: true,
	})

	manager.AddOrUpdateConfig("disabled1", config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: false,
	})

	stats := manager.GetStats()

	if stats["total"].(int) != 3 {
		t.Errorf("expected total 3, got %d", stats["total"])
	}

	if stats["enabled"].(int) != 2 {
		t.Errorf("expected enabled count 2, got %d", stats["enabled"])
	}

	if stats["disabled"].(int) != 1 {
		t.Errorf("expected disabled count 1, got %d", stats["disabled"])
	}
}

func TestExternalMCPManager_LoadConfigs(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	externalMCPConfig := config.ExternalMCPConfig{
		Servers: map[string]config.ExternalMCPServerConfig{
			"loaded1": {
				Command:           "python3",
				ExternalMCPEnable: true,
			},
			"loaded2": {
				URL:               "http://127.0.0.1:8081/mcp",
				ExternalMCPEnable: false,
			},
		},
	}

	manager.LoadConfigs(&externalMCPConfig)

	configs := manager.GetConfigs()
	if len(configs) != 2 {
		t.Fatalf("expected 2 configurations, got %d", len(configs))
	}

	if configs["loaded1"].Command != "python3" {
		t.Error("configuration 1 failed to load")
	}

	if configs["loaded2"].URL != "http://127.0.0.1:8081/mcp" {
		t.Error("configuration 2 failed to load")
	}
}

// TestLazySDKClient_InitializeFails verifies that with an invalid configuration the SDK client's Initialize fails and the error state is set
func TestLazySDKClient_InitializeFails(t *testing.T) {
	logger := zap.NewNop()
	// Use a non-existent HTTP address; Initialize should fail
	cfg := config.ExternalMCPServerConfig{
		Type:    "http",
		URL:     "http://127.0.0.1:19999/nonexistent",
		Timeout: 2,
	}
	c := newLazySDKClient(cfg, logger)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := c.Initialize(ctx)
	if err == nil {
		t.Fatal("expected error when connecting to invalid server")
	}
	if c.GetStatus() != "error" {
		t.Errorf("expected status error, got %s", c.GetStatus())
	}
	c.Close()
}

func TestExternalMCPManager_StartStopClient(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	// Add a disabled configuration
	cfg := config.ExternalMCPServerConfig{
		Command:           "python3",
		ExternalMCPEnable: false,
	}

	manager.AddOrUpdateConfig("test-start-stop", cfg)

	// Try to start (may fail because there is no real server)
	err := manager.StartClient("test-start-stop")
	if err != nil {
		t.Logf("startup failed (possibly no server): %v", err)
	}

	// Stop
	err = manager.StopClient("test-start-stop")
	if err != nil {
		t.Fatalf("failed to stop: %v", err)
	}

	// Verify the configuration was updated to disabled
	configs := manager.GetConfigs()
	if configs["test-start-stop"].ExternalMCPEnable {
		t.Error("the configuration should have been disabled")
	}
}

func TestExternalMCPManager_CallTool(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	// Test calling a non-existent tool
	_, _, err := manager.CallTool(context.Background(), "nonexistent::tool", map[string]interface{}{})
	if err == nil {
		t.Error("should return an error")
	}

	// Test an invalid tool-name format
	_, _, err = manager.CallTool(context.Background(), "invalid-tool-name", map[string]interface{}{})
	if err == nil {
		t.Error("should return an error (invalid format)")
	}
}

func TestExternalMCPManager_GetAllTools(t *testing.T) {
	logger := zap.NewNop()
	manager := NewExternalMCPManager(logger)

	ctx := context.Background()
	tools, err := manager.GetAllTools(ctx)
	if err != nil {
		t.Fatalf("failed to get the tool list: %v", err)
	}

	// If there are no connected clients, it should return an empty list
	if len(tools) != 0 {
		t.Logf("got %d tools", len(tools))
	}
}
