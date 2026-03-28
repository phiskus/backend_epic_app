package fhir

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"epic_lab_reporter/auth"
	"epic_lab_reporter/config"
)

// LabResult holds one Observation value with its context and abnormality flag.
type LabResult struct {
	PatientID      string
	ReportName     string // parent DiagnosticReport name
	TestName       string // Observation.code display
	Value          string // formatted numeric value + unit, e.g. "7.2 mmol/L"
	RangeLow       string // reference range low, e.g. "3.5"
	RangeHigh      string // reference range high, e.g. "5.1"
	RangeText      string // free-text range fallback, e.g. "< 200 mg/dL"
	Interpretation string // HL7 code: H, L, HH, LL, N, A  etc.
	IsAbnormal     bool   // true when High, Low, Critical, or outside range
	Date           string
	Status         string
}

// ── FHIR structs ─────────────────────────────────────────────────────────────

type fhirDiagnosticBundle struct {
	Entry []struct {
		Resource fhirDiagnosticReport `json:"resource"`
	} `json:"entry"`
}

type fhirDiagnosticReport struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Code   struct {
		Coding []struct {
			Display string `json:"display"`
		} `json:"coding"`
		Text string `json:"text"`
	} `json:"code"`
	EffectiveDateTime string `json:"effectiveDateTime"`
	Issued            string `json:"issued"`
	Result            []struct {
		Reference string `json:"reference"`
		Display   string `json:"display"`
	} `json:"result"`
}

type fhirObservation struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Code   struct {
		Coding []struct {
			Display string `json:"display"`
		} `json:"coding"`
		Text string `json:"text"`
	} `json:"code"`
	EffectiveDateTime string `json:"effectiveDateTime"`
	Issued            string `json:"issued"`
	// Numeric value
	ValueQuantity *struct {
		Value float64 `json:"value"`
		Unit  string  `json:"unit"`
	} `json:"valueQuantity"`
	// Categorical / text value fallbacks
	ValueString          string `json:"valueString"`
	ValueCodeableConcept *struct {
		Text string `json:"text"`
	} `json:"valueCodeableConcept"`
	// Abnormality flag from the lab system
	Interpretation []struct {
		Coding []struct {
			Code    string `json:"code"`
			Display string `json:"display"`
		} `json:"coding"`
	} `json:"interpretation"`
	// Reference range
	ReferenceRange []struct {
		Low *struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"low"`
		High *struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
		} `json:"high"`
		Text string `json:"text"`
	} `json:"referenceRange"`
}

// ── Public API ────────────────────────────────────────────────────────────────

// FetchLabs fetches DiagnosticReports + their Observation values for every
// patient concurrently using a pool of 5 goroutines.
// Returns a map of patientID → []LabResult.
func FetchLabs(cfg *config.Config, tc *auth.TokenClient, patients []PatientSummary) (map[string][]LabResult, error) {
	token, err := tc.GetToken()
	if err != nil {
		return nil, fmt.Errorf("get token for labs: %w", err)
	}

	type result struct {
		patientID string
		labs      []LabResult
		err       error
	}

	jobs := make(chan PatientSummary, len(patients))
	results := make(chan result, len(patients))

	const workers = 5
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				labs, err := fetchLabsForPatient(cfg.EpicFHIRBase, p.ID, token)
				results <- result{patientID: p.ID, labs: labs, err: err}
			}
		}()
	}

	for _, p := range patients {
		jobs <- p
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	labMap := make(map[string][]LabResult, len(patients))
	for r := range results {
		if r.err != nil {
			fmt.Printf("  WARNING: labs for patient %s: %v\n", r.patientID, r.err)
			continue
		}
		labMap[r.patientID] = r.labs
	}
	return labMap, nil
}

// SimulateHighHbA1c finds the first Hemoglobin A1c result across all patients
// and overrides its value to 9.2 % with a HIGH flag and realistic reference range.
// Used for testing the email alert path. Remove the call in scheduler.go when done.
func SimulateHighHbA1c(labMap map[string][]LabResult) {
	for pid, labs := range labMap {
		for i, l := range labs {
			if strings.Contains(strings.ToLower(l.TestName), "a1c") ||
				strings.Contains(strings.ToLower(l.TestName), "hemoglobin a1c") {
				labs[i].Value = "9.2 %"
				labs[i].RangeLow = "4.0"
				labs[i].RangeHigh = "5.6"
				labs[i].Interpretation = "H"
				labs[i].IsAbnormal = true
				labs[i].Status = "final"
				labMap[pid] = labs
				return
			}
		}
	}
}

// HasAbnormal returns true if any lab result across all patients is flagged abnormal.
func HasAbnormal(labMap map[string][]LabResult) bool {
	for _, labs := range labMap {
		for _, l := range labs {
			if l.IsAbnormal {
				return true
			}
		}
	}
	return false
}

// ── Private helpers ───────────────────────────────────────────────────────────

// fetchLabsForPatient fetches all DiagnosticReports for one patient, then
// follows each result reference to retrieve the individual Observation values.
func fetchLabsForPatient(fhirBase, patientID, token string) ([]LabResult, error) {
	url := fmt.Sprintf("%s/DiagnosticReport?patient=%s&category=laboratory&_count=50", fhirBase, patientID)
	body, err := doGet(url, token)
	if err != nil {
		return nil, fmt.Errorf("GET /DiagnosticReport: %w", err)
	}

	var bundle fhirDiagnosticBundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return nil, fmt.Errorf("decode DiagnosticReport bundle: %w", err)
	}

	var labs []LabResult
	for _, entry := range bundle.Entry {
		dr := entry.Resource
		rName := reportName(dr)
		rDate := reportDate(dr)

		// Follow each Observation reference inside the DiagnosticReport.
		for _, ref := range dr.Result {
			obsID := extractResourceID(ref.Reference)
			if obsID == "" {
				continue
			}
			obsURL := fmt.Sprintf("%s/Observation/%s", fhirBase, obsID)
			obsBody, err := doGet(obsURL, token)
			if err != nil {
				// Non-fatal: log and skip this observation.
				fmt.Printf("    skip Observation/%s: %v\n", obsID, err)
				continue
			}
			var obs fhirObservation
			if err := json.Unmarshal(obsBody, &obs); err != nil {
				continue
			}
			lab := buildLabResult(obs, patientID, rName, rDate)
			labs = append(labs, lab)
		}

		// If the DiagnosticReport had no result references, add a summary row.
		if len(dr.Result) == 0 {
			labs = append(labs, LabResult{
				PatientID:  patientID,
				ReportName: rName,
				TestName:   rName,
				Date:       rDate,
				Status:     dr.Status,
			})
		}
	}
	return labs, nil
}

// buildLabResult converts a FHIR Observation into a LabResult.
func buildLabResult(obs fhirObservation, patientID, reportName, reportDate string) LabResult {
	lab := LabResult{
		PatientID:  patientID,
		ReportName: reportName,
		TestName:   obsName(obs),
		Status:     obs.Status,
		Date:       obsDate(obs, reportDate),
	}

	// ── Value ────────────────────────────────────────────────────────────────
	if obs.ValueQuantity != nil {
		lab.Value = fmt.Sprintf("%.4g %s", obs.ValueQuantity.Value, strings.TrimSpace(obs.ValueQuantity.Unit))
	} else if obs.ValueString != "" {
		lab.Value = obs.ValueString
	} else if obs.ValueCodeableConcept != nil {
		lab.Value = obs.ValueCodeableConcept.Text
	}

	// ── Reference range ──────────────────────────────────────────────────────
	if len(obs.ReferenceRange) > 0 {
		rr := obs.ReferenceRange[0]
		if rr.Low != nil {
			lab.RangeLow = fmt.Sprintf("%.4g", rr.Low.Value)
		}
		if rr.High != nil {
			lab.RangeHigh = fmt.Sprintf("%.4g", rr.High.Value)
		}
		lab.RangeText = rr.Text
	}

	// ── Interpretation ───────────────────────────────────────────────────────
	if len(obs.Interpretation) > 0 && len(obs.Interpretation[0].Coding) > 0 {
		lab.Interpretation = obs.Interpretation[0].Coding[0].Code
		lab.IsAbnormal = isAbnormalCode(lab.Interpretation)
	}

	// Fallback: compare numeric value against reference range if no interpretation.
	if !lab.IsAbnormal && obs.ValueQuantity != nil && len(obs.ReferenceRange) > 0 {
		rr := obs.ReferenceRange[0]
		v := obs.ValueQuantity.Value
		if rr.Low != nil && v < rr.Low.Value {
			lab.IsAbnormal = true
			lab.Interpretation = "L"
		} else if rr.High != nil && v > rr.High.Value {
			lab.IsAbnormal = true
			lab.Interpretation = "H"
		}
	}

	return lab
}

// isAbnormalCode returns true for any HL7 interpretation code that means
// the value is outside the normal range.
func isAbnormalCode(code string) bool {
	switch strings.ToUpper(code) {
	case "H", "HH", "HU", "L", "LL", "LU", "A", "AA", "IE", "IND":
		return true
	}
	return false
}

// extractResourceID handles both relative ("Observation/abc") and absolute
// ("https://fhir.epic.com/.../Observation/abc") FHIR reference strings.
func extractResourceID(reference string) string {
	parts := strings.Split(reference, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-1]
	}
	return ""
}

// obsName returns the best display name for the observation.
func obsName(obs fhirObservation) string {
	if obs.Code.Text != "" {
		return obs.Code.Text
	}
	for _, c := range obs.Code.Coding {
		if c.Display != "" {
			return c.Display
		}
	}
	return "Unknown"
}

// obsDate returns the observation date, falling back to the parent report date.
func obsDate(obs fhirObservation, fallback string) string {
	if obs.EffectiveDateTime != "" {
		return obs.EffectiveDateTime
	}
	if obs.Issued != "" {
		return obs.Issued
	}
	return fallback
}

// reportName extracts the DiagnosticReport display name.
func reportName(r fhirDiagnosticReport) string {
	if r.Code.Text != "" {
		return r.Code.Text
	}
	for _, c := range r.Code.Coding {
		if c.Display != "" {
			return c.Display
		}
	}
	return "Unknown"
}

// reportDate returns effectiveDateTime, falling back to issued, then "unknown".
func reportDate(r fhirDiagnosticReport) string {
	if r.EffectiveDateTime != "" {
		return r.EffectiveDateTime
	}
	if r.Issued != "" {
		return r.Issued
	}
	return "unknown"
}
