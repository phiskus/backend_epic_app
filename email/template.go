package email

import (
	"bytes"
	"fmt"
	"html/template"
	"time"

	"epic_lab_reporter/fhir"
)

// ReportData is passed to the HTML template.
type ReportData struct {
	GeneratedAt   string
	Patients      []PatientReport
	TotalLabs     int
	TotalAbnormal int
}

// PatientReport groups a patient with their lab results.
type PatientReport struct {
	Name          string
	ID            string
	Labs          []fhir.LabResult
	AbnormalCount int
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Epic Lab Reporter — Daily Summary</title>
  <style>
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
      background: #f4f6f9;
      color: #1a1a2e;
      margin: 0;
      padding: 24px;
    }
    .container {
      max-width: 760px;
      margin: 0 auto;
      background: #ffffff;
      border-radius: 8px;
      overflow: hidden;
      box-shadow: 0 2px 8px rgba(0,0,0,0.08);
    }
    .header {
      background: #003087;
      color: #ffffff;
      padding: 28px 32px;
    }
    .header h1 { margin: 0 0 4px 0; font-size: 22px; font-weight: 600; }
    .header p  { margin: 0; font-size: 13px; opacity: 0.75; }
    .summary-bar {
      background: #eef2ff;
      padding: 14px 32px;
      font-size: 13px;
      color: #4a5568;
      border-bottom: 1px solid #e2e8f0;
    }
    .summary-bar strong { color: #003087; }
    .alert-badge {
      display: inline-block;
      background: #dc2626;
      color: white;
      border-radius: 12px;
      padding: 1px 10px;
      font-size: 12px;
      font-weight: 600;
      margin-left: 8px;
    }
    .patient-block {
      padding: 20px 32px;
      border-bottom: 1px solid #e2e8f0;
    }
    .patient-block:last-child { border-bottom: none; }
    .patient-name {
      font-size: 15px;
      font-weight: 600;
      color: #003087;
      margin: 0 0 10px 0;
    }
    table {
      width: 100%;
      border-collapse: collapse;
      font-size: 13px;
    }
    th {
      text-align: left;
      padding: 6px 10px;
      background: #f7f9fc;
      color: #6b7280;
      font-weight: 500;
      border-bottom: 1px solid #e2e8f0;
    }
    td {
      padding: 7px 10px;
      border-bottom: 1px solid #f0f0f0;
      vertical-align: middle;
    }
    tr:last-child td { border-bottom: none; }
    tr.abnormal { background: #fff5f5; }
    .interp {
      display: inline-block;
      border-radius: 4px;
      padding: 1px 7px;
      font-size: 11px;
      font-weight: 700;
      letter-spacing: 0.5px;
    }
    .interp-H  { background:#fee2e2; color:#b91c1c; }
    .interp-L  { background:#dbeafe; color:#1d4ed8; }
    .interp-A  { background:#fef9c3; color:#92400e; }
    .interp-N  { background:#dcfce7; color:#15803d; }
    .interp-unknown { background:#f1f5f9; color:#64748b; }
    .range { color: #9ca3af; font-size: 12px; }
    .value-abnormal { font-weight: 600; color: #b91c1c; }
    .status-final   { color: #16a34a; }
    .status-amended { color: #d97706; }
    .status-prelim  { color: #2563eb; }
    .no-labs { font-size: 13px; color: #9ca3af; font-style: italic; }
    .footer {
      padding: 16px 32px;
      font-size: 11px;
      color: #9ca3af;
      text-align: center;
      background: #f9fafb;
    }
  </style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1>Epic Lab Reporter</h1>
    <p>Daily diagnostic report summary — generated {{.GeneratedAt}}</p>
  </div>

  <div class="summary-bar">
    <strong>{{len .Patients}}</strong> patients &nbsp;·&nbsp;
    <strong>{{.TotalLabs}}</strong> observations &nbsp;·&nbsp;
    {{if gt .TotalAbnormal 0}}
      <strong style="color:#dc2626">⚠ {{.TotalAbnormal}} abnormal result{{if gt .TotalAbnormal 1}}s{{end}}</strong>
    {{else}}
      <strong style="color:#16a34a">✓ All results within range</strong>
    {{end}}
  </div>

  {{range .Patients}}
  <div class="patient-block">
    <p class="patient-name">
      {{.Name}}
      {{if gt .AbnormalCount 0}}<span class="alert-badge">{{.AbnormalCount}} abnormal</span>{{end}}
    </p>

    {{if .Labs}}
    <table>
      <thead>
        <tr>
          <th>Date</th>
          <th>Test</th>
          <th>Value</th>
          <th>Reference Range</th>
          <th>Flag</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {{range .Labs}}
        <tr{{if .IsAbnormal}} class="abnormal"{{end}}>
          <td>{{shortDate .Date}}</td>
          <td>{{.TestName}}</td>
          <td{{if .IsAbnormal}} class="value-abnormal"{{end}}>{{if .Value}}{{.Value}}{{else}}—{{end}}</td>
          <td class="range">{{rangeDisplay .RangeLow .RangeHigh .RangeText}}</td>
          <td>{{interpBadge .Interpretation}}</td>
          <td class="{{statusClass .Status}}">{{if .Status}}{{.Status}}{{else}}—{{end}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
    {{else}}
      <p class="no-labs">No lab reports found.</p>
    {{end}}
  </div>
  {{end}}

  <div class="footer">
    Generated by Epic Lab Reporter &nbsp;·&nbsp; Epic FHIR R4 Sandbox
  </div>
</div>
</body>
</html>`

// RenderHTML builds the HTML email body from patients and their lab results.
func RenderHTML(patients []fhir.PatientSummary, labMap map[string][]fhir.LabResult) (string, error) {
	funcMap := template.FuncMap{
		"shortDate": func(d string) string {
			if len(d) >= 10 {
				return d[:10]
			}
			if d == "" || d == "unknown" {
				return "—"
			}
			return d
		},
		"statusClass": func(s string) string {
			switch s {
			case "final":
				return "status-final"
			case "amended", "corrected":
				return "status-amended"
			case "preliminary":
				return "status-prelim"
			default:
				return ""
			}
		},
		"rangeDisplay": func(low, high, text string) string {
			if low != "" && high != "" {
				return fmt.Sprintf("%s – %s", low, high)
			}
			if low != "" {
				return "≥ " + low
			}
			if high != "" {
				return "≤ " + high
			}
			return text
		},
		"interpBadge": func(code string) template.HTML {
			if code == "" {
				return template.HTML(`<span class="interp interp-unknown">—</span>`)
			}
			label := interpLabel(code)
			cssClass := interpCSSClass(code)
			return template.HTML(fmt.Sprintf(`<span class="interp interp-%s">%s</span>`, cssClass, label))
		},
	}

	tmpl, err := template.New("report").Funcs(funcMap).Parse(htmlTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var patientReports []PatientReport
	totalLabs := 0
	totalAbnormal := 0
	for _, p := range patients {
		labs := labMap[p.ID]
		abnormal := 0
		for _, l := range labs {
			if l.IsAbnormal {
				abnormal++
			}
		}
		totalLabs += len(labs)
		totalAbnormal += abnormal
		patientReports = append(patientReports, PatientReport{
			Name:          p.Name,
			ID:            p.ID,
			Labs:          labs,
			AbnormalCount: abnormal,
		})
	}

	data := ReportData{
		GeneratedAt:   time.Now().Format("02 Jan 2006, 15:04 MST"),
		Patients:      patientReports,
		TotalLabs:     totalLabs,
		TotalAbnormal: totalAbnormal,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// interpLabel converts an HL7 interpretation code to a short readable label.
func interpLabel(code string) string {
	switch code {
	case "H":
		return "HIGH"
	case "HH":
		return "CRIT HIGH"
	case "HU":
		return "SIGNIFICANTLY HIGH"
	case "L":
		return "LOW"
	case "LL":
		return "CRIT LOW"
	case "LU":
		return "SIGNIFICANTLY LOW"
	case "N":
		return "NORMAL"
	case "A":
		return "ABNORMAL"
	case "AA":
		return "CRITICAL"
	default:
		return code
	}
}

// interpCSSClass maps an HL7 code to one of the defined CSS classes.
func interpCSSClass(code string) string {
	switch code {
	case "H", "HH", "HU":
		return "H"
	case "L", "LL", "LU":
		return "L"
	case "A", "AA":
		return "A"
	case "N":
		return "N"
	default:
		return "unknown"
	}
}
