package scheduler

import (
	"fmt"
	"log"
	"time"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
	"epic_lab_reporter/email"
	"epic_lab_reporter/fhir"
)


// Run executes the report job immediately, then repeats every cfg.SchedulerInterval.
// It blocks forever — call it in a goroutine if you need the main thread free.
func Run(cfg *config.Config, tc *auth.TokenClient) {
	// Fire immediately on startup so you don't wait 24h for the first report.
	runOnce(cfg, tc)

	ticker := time.NewTicker(cfg.SchedulerInterval)
	defer ticker.Stop()

	for range ticker.C {
		runOnce(cfg, tc)
	}
}

// runOnce fetches patients, fetches their labs, renders HTML and sends the email.
// Errors are logged but do not crash the service — the next tick will retry.
func runOnce(cfg *config.Config, tc *auth.TokenClient) {
	start := time.Now()
	log.Println("── scheduler: starting report run ──")

	patients, err := fhir.FetchPatients(cfg, tc)
	if err != nil {
		log.Printf("scheduler: fetch patients failed: %v", err)
		return
	}
	log.Printf("scheduler: %d patients retrieved", len(patients))

	labMap, err := fhir.FetchLabs(cfg, tc, patients)
	if err != nil {
		log.Printf("scheduler: fetch labs failed: %v", err)
		return
	}
	total := 0
	for _, labs := range labMap {
		total += len(labs)
	}
	log.Printf("scheduler: %d lab reports retrieved", total)

	// Simulation: force HbA1c to appear as critically high.
	// Remove this line when done testing.
	fhir.SimulateHighHbA1c(labMap)

	html, err := email.RenderHTML(patients, labMap)
	if err != nil {
		log.Printf("scheduler: render HTML failed: %v", err)
		return
	}

	if err := email.Send(cfg, html); err != nil {
		log.Printf("scheduler: send email failed: %v", err)
		return
	}

	log.Printf("scheduler: email sent to %s (took %s)", cfg.SMTPTo, time.Since(start).Round(time.Millisecond))
	fmt.Printf("✓ Report sent → %s\n", cfg.SMTPTo)
}
