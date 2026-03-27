# Epic Lab Reporter

A headless Go background service that fetches lab results (`DiagnosticReport`) for all patients in the Epic FHIR sandbox every 24 hours and sends a single HTML email summary via SMTP.

No browser. No UI. No user interaction. Pure backend service.

---

## How It Works

```
┌─────────────────────────────────────────────────────────────┐
│                      On every run (24h)                      │
│                                                             │
│  1. Build a signed JWT (RS256) using the RSA private key   │
│  2. POST to Epic token endpoint → receive access_token     │
│  3. Epic verifies JWT by fetching our JWKS endpoint        │
│  4. Bulk export all patients via FHIR Group/$export        │
│  5. Fetch DiagnosticReports per patient (5 goroutines)     │
│  6. Render HTML email and send via SMTP (Gmail)            │
│  7. Append summary to logs/audit.log                       │
└─────────────────────────────────────────────────────────────┘
```

### Auth Flow (SMART Backend Services)

This service uses the **SMART Backend Services** OAuth2 flow — the machine-to-machine equivalent of PKCE. There is no user login. Instead, a signed JWT is exchanged for an access token.

```
Our Service                          Epic Auth Server
    │
    ├─ Sign JWT with RSA private key
    │   iss / sub = CLIENT_ID
    │   aud        = token URL
    │   jku        = our JWKS URL   ← tells Epic where to fetch the public key
    │   kid        = key fingerprint
    │   exp        = now + 5 min
    │
    ├─ POST /oauth2/token ──────────────────────────────────►
    │   grant_type=client_credentials
    │   client_assertion=<signed JWT>
    │
    │              Epic fetches /.well-known/jwks.json ◄─────┤
    │              Epic verifies JWT signature               │
    │◄─ { access_token, expires_in } ───────────────────────┤
    │
    └─ GET /fhir/R4/Group/{id}/$export ─────────────────────►
        Authorization: Bearer <access_token>
```

### Why `jku` in the JWT Header Matters

The `jku` (JWK Set URL) header in the JWT tells Epic exactly where to fetch the JWKS — without it, Epic relies on its portal database cache, which can serve a stale/empty response if the URL was unreachable during a previous attempt. With `jku`, Epic fetches fresh on every request.

### Why `Cache-Control: no-store` on the JWKS Endpoint

Our JWKS handler responds with `Cache-Control: no-store`. This instructs Epic not to cache the public key response at all — it must fetch fresh every time. During development this is essential: if Epic caches a failed fetch (e.g. tunnel was down), it rejects tokens for up to an hour even after the tunnel comes back up.

---

## Project Structure

```
epic_lab_reporter/
  main.go                    — entry point; starts JWKS server + scheduler
  config/
    config.go                — loads .env into Config struct, validates fields
  auth/
    keys.go                  — RSA key loading; derives kid from SHA-256 fingerprint
    jwt.go                   — builds + RS256-signs JWT with jku/kid headers
    token.go                 — exchanges JWT for access token, caches, auto-refreshes
  fhir/
    patients.go              — FHIR Bulk Export ($export) → []PatientSummary
    labs.go                  — GET /DiagnosticReport per patient (5 goroutines)
  email/
    template.go              — renders HTML email body
    smtp.go                  — sends via Gmail STARTTLS (port 587)
  jwks/
    jwks.go                  — serves GET /.well-known/jwks.json
  scheduler/
    scheduler.go             — fires on startup + every SCHEDULER_INTERVAL
  keys/
    private.pem              ← gitignored — RSA-2048 private key
    public.pem               ← gitignored — RSA public key
  logs/
    audit.log                ← gitignored — appended on every run
  .env                       ← gitignored — your secrets
  .env.example               — committed template
  Dockerfile                 — multi-stage: golang:alpine → alpine:latest
  docker-compose.yml         — mounts keys/ and logs/ as volumes
```

---

## Prerequisites

| Tool | Purpose |
|------|---------|
| Go 1.21+ | Build and run locally |
| Docker Desktop | Build image, `docker compose up` |
| Google Cloud SDK (`gcloud`) | Cloud Run deployment |
| VS Code with Remote Tunnels extension | Stable public HTTPS URL for local dev |
| Epic developer account | App registration at `fhir.epic.com` |
| Gmail account with App Password | SMTP sending |

---

## One-Time Setup

### 1 — Generate RSA keypair

```bash
mkdir -p keys
openssl genrsa -out keys/private.pem 2048
openssl rsa -in keys/private.pem -pubout -out keys/public.pem
```

Keep `keys/private.pem` secret — it is gitignored and must never be committed.

### 2 — Create `.env` from template

```bash
cp .env.example .env
```

Edit `.env` with your values (see [Configuration Reference](#configuration-reference)).

### 3 — Register app in the Epic Developer Portal

1. Go to [fhir.epic.com](https://fhir.epic.com) → Sign in → **Build Apps** → **Create**
2. Choose **Backend Systems** app type
3. Select SMART version: **SMART v2**
4. Add authorized APIs: `Patient.read (System)` + `DiagnosticReport.read (System)`
5. Under **Non-Production JWK Set URL** — paste your public JWKS URL (see options below)
6. Copy the **Non-Production Client ID** → paste into `.env` as `EPIC_CLIENT_ID`

> The JWKS URL must be publicly reachable over HTTPS. Epic fetches it on every token request to verify your JWT signature.

### 4 — Set up Gmail App Password

1. Google Account → Security → 2-Step Verification → **App Passwords**
2. Generate a password for "Mail"
3. Paste the 16-character result as `SMTP_PASS` in `.env`

---

## Running Locally (VS Code DevTunnel)

The JWKS endpoint must be publicly reachable so Epic can verify your JWT. VS Code DevTunnels gives you a stable HTTPS URL that forwards to your local port 8080.

### Step 1 — Start the DevTunnel

1. Open VS Code
2. Open the **Ports** panel (bottom status bar → **Ports** tab, or `Ctrl+Shift+P` → "Focus on Ports View")
3. Click **Forward a Port** → enter `8080`
4. Right-click the forwarded port → **Port Visibility** → set to **Public**
5. Copy the generated URL — it looks like `https://xxxx-8080.euw.devtunnels.ms`

### Step 2 — Update `.env`

```env
EPIC_JWKS_URL=https://xxxx-8080.euw.devtunnels.ms/.well-known/jwks.json
```

### Step 3 — Update the Epic portal

Paste the new JWKS URL into the **Non-Production JWK Set URL** field of your app in the Epic portal and save. Epic will use this URL from that point forward.

### Step 4 — Run

```bash
go run .
```

You will see:
```
Config loaded ✓
JWKS server listening on :8080 ✓
JWT:
eyJhbGci...

2026/03/27 21:48:57 JWKS HIT: GET /.well-known/jwks.json — User-Agent: Epic-Interconnect/117.0...
Access token ✓  eyJhbGciOiJSUzI1NiIs...
Bulk export started — polling https://...
....
Patients ✓  found 7
  [abc123] Doe, John  MRN:1234
  ...
```

> If you restart VS Code the DevTunnel URL may change. If it does, update both `.env` and the Epic portal registration.

---

## Running with Docker (local)

```bash
docker compose up --build
```

`docker-compose.yml` mounts `./keys` and `./logs` as volumes — keys are never baked into the image.

To override the scheduler interval for testing:

```bash
SCHEDULER_INTERVAL=1m docker compose up --build
```

---

## Deploying to Google Cloud Run

Cloud Run gives you a **permanent public HTTPS URL** — ideal for registering in the Epic portal once and never changing it.

### Step 1 — Project setup (one-time)

```bash
gcloud auth login
gcloud config set project epic-lab-reporter-2026
```

### Step 2 — Store RSA keys in Secret Manager

```bash
gcloud secrets create epic-private-key \
  --data-file=keys/private.pem \
  --project=epic-lab-reporter-2026

gcloud secrets create epic-public-key \
  --data-file=keys/public.pem \
  --project=epic-lab-reporter-2026
```

Grant the default compute service account access to the secrets:

```bash
PROJECT_NUMBER=$(gcloud projects describe epic-lab-reporter-2026 --format="value(projectNumber)")

gcloud projects add-iam-policy-binding epic-lab-reporter-2026 \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

### Step 3 — Create Artifact Registry and push image

```bash
# Create registry (one-time)
gcloud artifacts repositories create lab-reporter \
  --repository-format=docker \
  --location=us-central1 \
  --project=epic-lab-reporter-2026

# Authenticate Docker
gcloud auth configure-docker us-central1-docker.pkg.dev

# Build and push
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --project=epic-lab-reporter-2026
```

### Step 4 — Deploy to Cloud Run

```bash
gcloud run deploy epic-lab-reporter \
  --image us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --port 8080 \
  --set-env-vars "\
EPIC_CLIENT_ID=your-client-id,\
EPIC_FHIR_BASE=https://fhir.epic.com/interconnect-fhir-oauth/api/FHIR/R4,\
EPIC_TOKEN_URL=https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token,\
EPIC_JWKS_URL=https://YOUR-SERVICE-URL/.well-known/jwks.json,\
EPIC_GROUP_ID=your-group-id,\
EPIC_PRIVATE_KEY_PATH=/app/keys-priv/private.pem,\
EPIC_PUBLIC_KEY_PATH=/app/keys-pub/public.pem,\
SMTP_HOST=smtp.gmail.com,\
SMTP_USER=your@gmail.com,\
SMTP_PASS=your-app-password,\
SMTP_TO=recipient@email.com,\
SMTP_FROM=Epic Lab Reporter <your@gmail.com>" \
  --update-secrets \
    "/app/keys-priv/private.pem=epic-private-key:latest,\
/app/keys-pub/public.pem=epic-public-key:latest" \
  --project=epic-lab-reporter-2026
```

Cloud Run outputs a URL like `https://epic-lab-reporter-xxxx-uc.a.run.app`.

### Step 5 — Verify the JWKS endpoint

```bash
curl https://epic-lab-reporter-xxxx-uc.a.run.app/.well-known/jwks.json
```

Expected response:
```json
{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"...","n":"...","e":"AQAB"}]}
```

### Step 6 — Register in Epic portal

Paste `https://epic-lab-reporter-xxxx-uc.a.run.app/.well-known/jwks.json` into the **Non-Production JWK Set URL** field and save. Update `EPIC_JWKS_URL` in Cloud Run env vars to match.

---

## Configuration Reference

| Variable | Required | Description |
|----------|----------|-------------|
| `EPIC_CLIENT_ID` | Yes | Non-Production Client ID from Epic portal |
| `EPIC_FHIR_BASE` | Yes | Epic FHIR R4 base URL |
| `EPIC_TOKEN_URL` | Yes | Epic OAuth2 token endpoint |
| `EPIC_PRIVATE_KEY_PATH` | Yes | Path to `private.pem` |
| `EPIC_PUBLIC_KEY_PATH` | Yes | Path to `public.pem` |
| `EPIC_JWKS_URL` | Yes | Public HTTPS URL Epic will fetch for JWKS |
| `EPIC_GROUP_ID` | Yes | FHIR Group ID for bulk patient export |
| `PORT` | No | HTTP listen port (default: `8080`) |
| `SMTP_HOST` | Yes | SMTP server hostname |
| `SMTP_PORT` | No | SMTP port (default: `587`) |
| `SMTP_USER` | Yes | SMTP username / Gmail address |
| `SMTP_PASS` | Yes | SMTP password / Gmail App Password |
| `SMTP_TO` | Yes | Recipient email address |
| `SMTP_FROM` | Yes | Sender display name and address |
| `SCHEDULER_INTERVAL` | No | Override 24h default (e.g. `1m` for testing) |

---

## Troubleshooting

### `invalid_client` from Epic token endpoint

| Symptom | Cause | Fix |
|---------|-------|-----|
| `ECFRequestCount:0` in response headers | Epic not fetching JWKS | Ensure `jku` header is in the JWT and the URL is reachable |
| `DBTime:0` | Epic not hitting its DB at all | Wrong `client_id` or wrong token URL |
| Works once, fails later | JWKS URL changed (DevTunnel restarted) | Update `.env` + Epic portal with new URL |
| Works locally, fails on Cloud Run | Keys not mounted | Verify Secret Manager mounts with `gcloud run services describe` |

### JWKS endpoint returns 404

The DevTunnel is not running or port visibility is set to **Private**. In VS Code Ports panel, confirm the port is listed and visibility is **Public**.

### Patients ✓ found 0

The FHIR Group ID is wrong or the `system/Patient.read` scope was not granted. Confirm `EPIC_GROUP_ID` in `.env` matches the Group in the Epic portal.

### Email not received

- Check spam folder
- Confirm Gmail App Password (not your account password) is used for `SMTP_PASS`
- Verify 2-Step Verification is enabled on the Google account

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/golang-jwt/jwt/v5` | RS256 JWT signing |
| `github.com/joho/godotenv` | `.env` file loading |

All other packages are Go stdlib (`net/http`, `net/smtp`, `crypto/rsa`, `encoding/json`, `time`, `log/slog`).
