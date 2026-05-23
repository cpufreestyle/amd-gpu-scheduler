package main

import (
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/hybrid-gpu-scheduler/pkg/agent"
	"github.com/hybrid-gpu-scheduler/pkg/types"
)

func main() {
	cfg := parseArgs()

	log.Printf("[agent] Node: %s", cfg.NodeName)
	log.Printf("[agent] Master: %s", cfg.MasterURL)
	log.Printf("[agent] Heartbeat: every %ds", cfg.HeartbeatSec)
	log.Printf("[agent] Local API: :%d", cfg.API_PORT)

	a := agent.New(cfg)

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[agent] Shutting down...")
		a.Stop()
		os.Exit(0)
	}()

	if err := a.Run(); err != nil {
		log.Fatalf("[agent] Fatal: %v", err)
	}
}

func parseArgs() types.AgentConfig {
	cfg := types.AgentConfig{
		HeartbeatSec: 5,
		API_PORT:     8081,
	}

	if v := os.Getenv("MASTER_URL"); v != "" {
		cfg.MasterURL = v
	} else {
		log.Fatal("MASTER_URL environment variable is required")
	}

	cfg.NodeName = os.Getenv("AGENT_NAME")
	cfg.NodeRegion = os.Getenv("AGENT_REGION")

	if v := os.Getenv("AGENT_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.API_PORT = p
		}
	}

	return cfg
}
