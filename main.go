package main

import (
	"log"
	"net/http"
	"time"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
	"epic_lab_reporter/jwks"
	"epic_lab_reporter/scheduler"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}
	log.Println("config loaded ✓")

	// Start the JWKS HTTP server so Epic can verify JWT signatures.
	// Must be running before the first token request.
	jwksHandler, err := jwks.Handler(cfg.EpicPublicKeyPath)
	if err != nil {
		log.Fatalf("jwks handler: %v", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("JWKS request: %s %s — %s", r.Method, r.URL.String(), r.Header.Get("User-Agent"))
		jwksHandler(w, r)
	})
	go func() {
		if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
			log.Fatalf("http server: %v", err)
		}
	}()
	time.Sleep(200 * time.Millisecond)
	log.Printf("JWKS server listening on :%s ✓", cfg.Port)

	// Token client — shared across all FHIR calls, handles caching + refresh.
	tokenClient := auth.NewTokenClient(cfg)

	// Warm up: verify we can get a token before entering the scheduler loop.
	if _, err := tokenClient.GetToken(); err != nil {
		log.Fatalf("initial token fetch failed: %v\n"+
			"Check EPIC_JWKS_URL is publicly reachable and matches the Epic portal registration.", err)
	}
	log.Println("access token ✓")

	// Run the report job now, then every cfg.SchedulerInterval (default 24h).
	log.Printf("scheduler starting — interval: %s", cfg.SchedulerInterval)
	scheduler.Run(cfg, tokenClient) // blocks forever
}
