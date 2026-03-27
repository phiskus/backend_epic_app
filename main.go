package main

import (
	"fmt"
	"log"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	fmt.Println("Config loaded ✓")

	// --- Step 2 smoke-test: build a JWT assertion ---
	privKey, err := auth.LoadPrivateKey(cfg.EpicPrivateKeyPath)
	if err != nil {
		log.Fatalf("load private key: %v", err)
	}

	pubKey, err := auth.LoadPublicKey(cfg.EpicPublicKeyPath)
	if err != nil {
		log.Fatalf("load public key: %v", err)
	}

	kid, err := auth.DeriveKID(pubKey)
	if err != nil {
		log.Fatalf("derive kid: %v", err)
	}
	fmt.Printf("Key ID (kid): %s\n", kid)

	assertion, err := auth.BuildAssertion(cfg, privKey, kid)
	if err != nil {
		log.Fatalf("build assertion: %v", err)
	}
	fmt.Printf("JWT assertion built ✓\n  %s...\n", assertion[:60])
}
