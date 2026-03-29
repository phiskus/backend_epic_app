package main

import (
	"log"
	"net/http"
	
	"os"
	"fmt"

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

	tokenClient := auth.NewTokenClient(cfg)

	if _, err := tokenClient.GetToken(); err != nil {
	log.Fatalf("initial token fetch failed: %v", err)
	}
	log.Println("access token ✓")



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
		// /run is called by Cloud Scheduler every 24h.
		// Locally, trigger it manually: curl -X POST http://localhost:8080/run
	mux.HandleFunc("/run", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		secret := os.Getenv("RUN_SECRET")
		if secret != "" && r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		go scheduler.RunOnce(cfg, tokenClient)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintln(w, "report run started")
	})

	log.Printf("server ready on :%s ✓", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatalf("http server: %v", err)
	}


		
}
