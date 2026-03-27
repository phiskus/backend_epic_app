# Epic Lab Reporter — Module Four

## Project Goal
A headless Go background service that:
1. Runs automatically every 24 hours (also fires immediately on startup)
2. Fetches all `DiagnosticReport` (lab) resources for every patient in the Epic sandbox
3. Sends a single HTML email via SMTP summarising all reports — normal and abnormal

No browser, no UI, no user interaction. Pure backend service.

---

## Background: What Was Built Before
- **Module Two** (`module_two/epic_pat_app`): Svelte 5 + TypeScript browser app, Epic EHR launch, PKCE OAuth, reads Patient + Observations
- **Module Three** (`module_three/cerner_pis/cerner_pis_app`): Svelte 5 + TypeScript browser app, Cerner EHR launch, PKCE OAuth, reads + writes Observations (vitals), reads DiagnosticReports + MedicationRequests, clinical dashboard UI with sidebar navigation

Module Four is fundamentally different: **no browser, no PKCE, no UI**. It is a confidential backend client.

---

## Key Architecture Decisions

### Why Go
- Single binary deployment (`go build` → one executable, no runtime)
- Stdlib `net/http` and `net/smtp` are production-grade — no heavy framework needed
- `encoding/json` maps cleanly to FHIR JSON structures
- Built-in concurrency (goroutines) for parallel patient fetching
- `log/slog` (Go 1.21+) for structured file logging — no external logging lib needed

### Why NOT PKCE here
PKCE is for **user-launched browser apps** (modules two and three). This service runs unattended on a schedule, so we use **SMART Backend Services** — a machine-to-machine OAuth2 flow where a signed JWT replaces the user. No browser redirect, no user consent screen.

### Auth: SMART Backend Services (JWT Client Credentials)
```
Service                              Epic Auth Server
  │
  ├─ Build JWT (RS256):
  │   iss = CLIENT_ID
  │   sub = CLIENT_ID
  │   aud = EPIC_TOKEN_URL
  │   jti = uuid  ← prevents replay attacks
  │   exp = now + 5 min
  │   Signed with RSA-256 private key
  │
  ├─ POST /oauth2/token ───────────────────────────►
  │   grant_type=client_credentials
  │   client_assertion_type=
  │     urn:ietf:params:oauth:client-assertion-type:jwt-bearer
  │   client_assertion=<signed_jwt>
  │   scope=system/DiagnosticReport.read system/Patient.read
  │
  │◄─ { access_token, expires_in } ───────────────┤
  │
  └─ GET /DiagnosticReport?... ───────────────────►  Epic FHIR R4
      Authorization: Bearer <access_token>
```

Token is cached in memory and refreshed automatically 60 seconds before expiry.

### Epic Sandbox Endpoints
- FHIR base: `https://fhir.epic.com/interconnect-fhir-oauth/api/FHIR/R4`
- Token URL: `https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token`
- App registration: `fhir.epic.com` → Build Apps → Backend Systems
- Required scopes: `system/Patient.read system/DiagnosticReport.read`
- Auth method: RSA-256 public key uploaded to Epic portal (no client secret used)

### Email
- SMTP with STARTTLS (port 587)
- One email per run — all patients in a single message
- HTML format, reports grouped by patient name
- Abnormal results highlighted in red
- Gmail App Password recommended for `SMTP_PASS` (not your account password)

### Scheduling
- `time.Ticker` with configurable interval (default 24h)
- First job fires immediately on startup — don't wait for the first tick
- `SCHEDULER_INTERVAL` env var overrides (e.g. `1m` for testing without waiting 24h)

### Concurrency pattern
- Patient list fetched first, sequentially
- Lab reports fetched with max **5 parallel goroutines** (semaphore channel pattern)
- Prevents Epic rate limiting while still being fast

### Logging
- All FHIR requests + response summaries appended to `logs/audit.log`
- Format: `[ISO timestamp] METHOD /endpoint → HTTP status (duration ms)`
- File and `logs/` directory created automatically on first run
- Errors written to both `audit.log` and stderr

---

## File Structure

```
epic_lab_reporter/
  main.go                    — entry point; wires all packages, starts scheduler
  config/
    config.go                — loads .env into Config struct, validates required fields
  auth/
    jwt.go                   — builds + RS256-signs the JWT assertion for Epic
    token.go                 — exchanges JWT for access token, caches, auto-refreshes
  fhir/
    patients.go              — GET /Patient?_count=100 → []PatientSummary
    labs.go                  — GET /DiagnosticReport?patient={id} → []LabReport
  email/
    template.go              — renders HTML email body from []PatientLabSummary
    smtp.go                  — sends via net/smtp (STARTTLS, port 587)
  scheduler/
    scheduler.go             — time.Ticker wrapper; fires job on startup + every interval
  logs/
    .gitkeep                 — ensures directory is committed; audit.log is gitignored
  keys/
    private.pem              — RSA-2048 private key  ← NEVER commit, gitignored
    public.pem               — RSA public key        ← upload to Epic portal
  .env                       — runtime secrets, gitignored
  .env.example               — committed template (no real values)
  .gitignore
  go.mod
  go.sum
```

---

## .env Template

```env
EPIC_CLIENT_ID=your-client-id-from-epic-portal
EPIC_FHIR_BASE=https://fhir.epic.com/interconnect-fhir-oauth/api/FHIR/R4
EPIC_TOKEN_URL=https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token
EPIC_PRIVATE_KEY_PATH=./keys/private.pem

SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your@gmail.com
SMTP_PASS=xxxx-xxxx-xxxx-xxxx
SMTP_TO=recipient@email.com
SMTP_FROM=Epic Lab Reporter <your@gmail.com>

# Remove the comment below to override the 24h default during testing
# SCHEDULER_INTERVAL=1m
```

---

## Go Dependencies

```
github.com/golang-jwt/jwt/v5   — RS256 JWT signing
github.com/joho/godotenv        — .env file loading
```

All other packages are Go stdlib (`net/http`, `net/smtp`, `crypto/rsa`, `encoding/json`, `time`, `log/slog`).

Setup commands:
```bash
go mod init epic_lab_reporter
go get github.com/golang-jwt/jwt/v5
go get github.com/joho/godotenv
```

---

## Pre-Requisites (complete before writing code)

### 1. Generate RSA keypair
```bash
mkdir -p keys
openssl genrsa -out keys/private.pem 2048
openssl rsa -in keys/private.pem -pubout -out keys/public.pem
```

### 2. Register app in Epic sandbox
1. Go to `fhir.epic.com` → Sign in → **Build Apps** → **Create**
2. Choose **"Backend Systems"** app type
3. Add authorized APIs: `Patient.read (System)` + `DiagnosticReport.read (System)`
4. Under **Public Keys** → **Add** → paste contents of `keys/public.pem`
5. Copy the **Client ID** into `.env` as `EPIC_CLIENT_ID`
6. Leave app in Development / sandbox mode

### 3. Set up Gmail SMTP
1. Google Account → Security → 2-Step Verification → **App Passwords**
2. Generate a password for "Mail"
3. Paste the 16-character result as `SMTP_PASS` in `.env`

---

## Job Execution Flow (runs every 24h)

```
startup
  │
  ├─ load config from .env
  ├─ read private key from disk
  ├─ [loop every SCHEDULER_INTERVAL]
  │     │
  │     ├─ build + sign JWT
  │     ├─ POST /oauth2/token → get access_token
  │     ├─ GET /Patient?_count=100 → patient list
  │     ├─ for each patient (max 5 goroutines):
  │     │     └─ GET /DiagnosticReport?patient={id}&category=laboratory
  │     ├─ build HTML email (group by patient, flag ABNORMAL rows)
  │     ├─ send via SMTP
  │     └─ append summary to logs/audit.log
```

---

## Email Format

```
Subject: Lab Report Summary — 2026-03-27 09:00 UTC

Patient: John Doe (MRN 12345)          3 reports
──────────────────────────────────────────────────
  Hemoglobin A1c     8.2 %         2026-03-26   ⚠ ABNORMAL
  Potassium          4.1 mEq/L     2026-03-26     normal
  Sodium             138 mEq/L     2026-03-25     normal

Patient: Jane Smith (MRN 67890)        1 report
──────────────────────────────────────────────────
  Complete Blood Count  —          2026-03-24     final
```

---

## Implementation Order

Build one file at a time. Test each milestone before moving on.

| Step | File | Milestone |
|------|------|-----------|
| 1 | `config/config.go` | `go run .` prints loaded config without error |
| 2 | `auth/jwt.go` | builds a JWT struct with correct claims |
| 3 | `auth/token.go` | `go run .` prints a real Epic access token |
| 4 | `fhir/patients.go` | `go run .` prints sandbox patient names |
| 5 | `fhir/labs.go` | `go run .` prints lab report count per patient |
| 6 | `email/template.go` | produces correct HTML string from mock data |
| 7 | `email/smtp.go` | email arrives in inbox |
| 8 | `scheduler/scheduler.go` | job runs on startup + repeats on interval |
| 9 | `main.go` | full end-to-end run, `audit.log` populated |

---

## User Preferences (established during module three)
- Walk through implementation **one file at a time** — don't write everything at once
- **Test at each step** before moving to the next
- Concise responses — no trailing summaries
- Explicit error handling — no silent `_` discards on errors
- Minimal dependencies — prefer stdlib; add a package only when stdlib genuinely can't do it
- No global mutable state beyond what is structurally necessary
- Partial failures must not crash the full run (equivalent of Go's `errgroup` with tolerance)
- Model recommendation for this project: **Claude Sonnet 4.6** (well-defined engineering task)
