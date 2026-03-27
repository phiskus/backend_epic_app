package main

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
	"epic_lab_reporter/fhir"
	"epic_lab_reporter/jwks"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	fmt.Println("Config loaded ✓")

	// Start JWKS HTTP server first so Epic can reach it
	jwksHandler, err := jwks.Handler(cfg.EpicPublicKeyPath)
	if err != nil {
		log.Fatalf("jwks handler: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("JWKS HIT: %s %s — User-Agent: %s", r.Method, r.URL.String(), r.Header.Get("User-Agent"))
		jwksHandler(w, r)
	})
	go func() {
		if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
			log.Fatalf("http server: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	fmt.Printf("JWKS server listening on :%s ✓\n", cfg.Port)

	// DEBUG: print JWT assertion for curl testing
	privKey, _ := auth.LoadPrivateKey(cfg.EpicPrivateKeyPath)
	pubKey, _ := auth.LoadPublicKey(cfg.EpicPublicKeyPath)
	kid, _ := auth.DeriveKID(pubKey)
	assertion, _ := auth.BuildAssertion(cfg, privKey, kid)
	fmt.Printf("JWT:\n%s\n\n", assertion)

	// Token client
	tokenClient := auth.NewTokenClient(cfg)
	token, err := tokenClient.GetToken()
	if err != nil {
		log.Printf("WARNING: get token failed: %v", err)
		log.Println("JWKS server still running — update Epic portal URL then retry.")
		select {} // block forever so tunnel stays alive
	}
	fmt.Printf("Access token ✓  %.20s...\n", token)

	// Step 5: fetch patients
	patients, err := fhir.FetchPatients(cfg, tokenClient)
	if err != nil {
		log.Fatalf("fetch patients: %v", err)
	}
	fmt.Printf("Patients ✓  found %d\n", len(patients))
	for _, p := range patients {
		fmt.Printf("  [%s] %s  MRN:%s\n", p.ID, p.Name, p.MRN)
	}
}
