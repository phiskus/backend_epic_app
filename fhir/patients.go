package fhir

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
)

// PatientSummary holds the fields we need from a FHIR Patient resource.
type PatientSummary struct {
	ID   string // FHIR resource ID
	Name string // Display name (family, given)
	MRN  string // Medical record number (if available)
}

// bulkManifest is the 200-OK body returned when a bulk export job completes.
type bulkManifest struct {
	Output []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"output"`
}

// fhirPatient is a minimal FHIR Patient resource.
type fhirPatient struct {
	ID   string `json:"id"`
	Name []struct {
		Family string   `json:"family"`
		Given  []string `json:"given"`
	} `json:"name"`
	Identifier []struct {
		Type struct {
			Coding []struct {
				Code string `json:"code"`
			} `json:"coding"`
		} `json:"type"`
		Value string `json:"value"`
	} `json:"identifier"`
}

// FetchPatients triggers a FHIR Bulk Data Export on the configured Group,
// polls until the job completes, then downloads and parses the Patient NDJSON.
func FetchPatients(cfg *config.Config, tc *auth.TokenClient) ([]PatientSummary, error) {
	token, err := tc.GetToken()
	if err != nil {
		return nil, fmt.Errorf("get token: %w", err)
	}

	// Step 1: kick off the async bulk export for Patient resources only.
	exportURL := cfg.EpicFHIRBase + "/Group/" + cfg.EpicGroupID + "/$export?_type=Patient"
	req, err := http.NewRequest(http.MethodGet, exportURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build $export request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/fhir+json")
	req.Header.Set("Prefer", "respond-async")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("$export request: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("$export returned %d, expected 202", resp.StatusCode)
	}

	pollURL := resp.Header.Get("Content-Location")
	if pollURL == "" {
		return nil, fmt.Errorf("$export response missing Content-Location header")
	}
	fmt.Printf("Bulk export started — polling %s\n", pollURL)

	// Step 2: poll the Content-Location URL until the job finishes (200 OK).
	manifest, err := pollBulkExport(pollURL, token)
	if err != nil {
		return nil, fmt.Errorf("poll $export: %w", err)
	}

	// Step 3: find the Patient output file and download it as NDJSON.
	var patients []PatientSummary
	for _, out := range manifest.Output {
		if out.Type != "Patient" {
			continue
		}
		ps, err := downloadPatientNDJSON(out.URL, token)
		if err != nil {
			return nil, fmt.Errorf("download Patient NDJSON: %w", err)
		}
		patients = append(patients, ps...)
	}
	return patients, nil
}

// pollBulkExport polls the given URL every 3 seconds until the server responds
// with 200 OK (job complete) or a non-202 error. Timeout: 5 minutes.
func pollBulkExport(pollURL, token string) (*bulkManifest, error) {
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, pollURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}

		switch resp.StatusCode {
		case http.StatusOK:
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				return nil, fmt.Errorf("read manifest: %w", err)
			}
			var m bulkManifest
			if err := json.Unmarshal(body, &m); err != nil {
				return nil, fmt.Errorf("decode manifest: %w", err)
			}
			return &m, nil

		case http.StatusAccepted:
			// Job still in progress — back off and retry.
			resp.Body.Close()
			fmt.Print(".")
			time.Sleep(3 * time.Second)

		default:
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return nil, fmt.Errorf("poll returned %d: %s", resp.StatusCode, string(body))
		}
	}
	return nil, fmt.Errorf("$export timed out after 5 minutes")
}

// downloadPatientNDJSON downloads an NDJSON file and parses each line as a Patient.
func downloadPatientNDJSON(url, token string) ([]PatientSummary, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/fhir+ndjson")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("NDJSON download returned %d: %s", resp.StatusCode, string(body))
	}

	var patients []PatientSummary
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var p fhirPatient
		if err := json.Unmarshal(line, &p); err != nil {
			continue // skip malformed lines
		}
		patients = append(patients, PatientSummary{
			ID:   p.ID,
			Name: displayName(p),
			MRN:  mrn(p),
		})
	}
	return patients, scanner.Err()
}

// doGet performs an authenticated GET and returns the body, or an error for non-200.
// Used by labs.go for per-patient DiagnosticReport queries.
func doGet(url, token string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/fhir+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("returned %d: %s", resp.StatusCode, string(body))
	}
	return body, nil
}

// displayName builds "Family, Given" from the first name entry.
func displayName(p fhirPatient) string {
	if len(p.Name) == 0 {
		return "Unknown"
	}
	n := p.Name[0]
	given := ""
	if len(n.Given) > 0 {
		given = n.Given[0]
	}
	if n.Family == "" {
		return given
	}
	if given == "" {
		return n.Family
	}
	return n.Family + ", " + given
}

// mrn extracts the MRN identifier (type code "MR") if present.
func mrn(p fhirPatient) string {
	for _, id := range p.Identifier {
		for _, coding := range id.Type.Coding {
			if coding.Code == "MR" {
				return id.Value
			}
		}
	}
	return ""
}
