package main

import (
	"fmt"
	"log"

	"epic_lab_reporter/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	fmt.Printf("Config loaded ✓\n")
	fmt.Printf("  Epic client: %s\n", cfg.EpicClientID)
	fmt.Printf("  FHIR base:   %s\n", cfg.EpicFHIRBase)
	fmt.Printf("  Port:        %s\n", cfg.Port)
	fmt.Printf("  Interval:    %s\n", cfg.SchedulerInterval)
	fmt.Printf("  SMTP to:     %s\n", cfg.SMTPTo)
}
