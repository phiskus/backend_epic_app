# Production Deployment Plan — Google Cloud Run

> **Status:** Plan only. No code has been changed.
> Work through each step in order. Each step has a verification check before proceeding.

---

## Architecture: What Changes for Production

The local version runs a `time.Ticker` that blocks forever inside the process.
Cloud Run is designed for HTTP-triggered, stateless containers — it can scale to zero
when idle, which means the internal ticker approach does not translate directly.

The production architecture splits responsibility cleanly:

```
┌─────────────────────────────────────────────────────────────────────┐
│                        Google Cloud                                  │
│                                                                     │
│   Cloud Scheduler (every 24h)                                       │
│         │                                                           │
│         │  POST /run   ┌──────────────────────────────────────┐    │
│         └─────────────►│  Cloud Run Service                   │    │
│                         │                                      │    │
│                         │  GET /.well-known/jwks.json  (JWKS) │    │
│                         │  POST /run  (trigger lab job)       │◄───┤ Epic
│                         │                                      │    │ fetches
│                         │  On /run:                           │    │ JWKS here
│                         │   1. fetch token (Epic fetches JWKS)│    │
│                         │   2. bulk export patients           │    │
│                         │   3. fetch observations             │    │
│                         │   4. render HTML                    │    │
│                         │   5. send email if abnormal         │    │
│                         │   6. return 200 OK                  │    │
│                         └──────────────────────────────────────┘    │
│                                                                     │
│   Secret Manager                                                    │
│     epic-private-key  (private.pem)                                 │
│     epic-public-key   (public.pem)                                  │
└─────────────────────────────────────────────────────────────────────┘
```

**Key differences from local:**

| Local | Production |
|-------|-----------|
| `time.Ticker` in main.go drives the 24h loop | Cloud Scheduler calls `POST /run` every 24h |
| Keys read from `./keys/*.pem` | Keys mounted from Secret Manager |
| JWKS URL is a DevTunnel (changes on restart) | JWKS URL is the permanent Cloud Run URL |
| No authentication on `/run` | `/run` protected by Cloud Run IAM (only Cloud Scheduler service account) |
| `SCHEDULER_INTERVAL` env var | Cron expression in Cloud Scheduler |

**Why `--min-instances=1`:**
When Epic calls the token endpoint, it immediately turns around and fetches our JWKS URL.
If the Cloud Run container is cold (scaled to zero), it may not respond in time and Epic
will return `invalid_client`. Setting `--min-instances=1` keeps one instance always warm.
Cost: approximately $5–10/month.

---

## Required Code Changes Before Deploying

> Do not deploy until these changes are made and tested locally.

### 1. Refactor `main.go` — replace ticker with HTTP handlers

`main.go` currently calls `scheduler.Run()` which blocks on a `time.Ticker`.
For Cloud Run, replace this with two HTTP routes:

- `GET /.well-known/jwks.json` → existing JWKS handler (no change)
- `POST /run` → trigger one full lab report cycle, return `200 OK` on success

The `/run` handler calls `scheduler.RunOnce()` (rename the existing `runOnce` to exported).
The process stays alive waiting for HTTP requests — Cloud Run handles the lifecycle.

### 2. Remove `SCHEDULER_INTERVAL` from config as a required field

It is only relevant locally. In production, Cloud Scheduler replaces it entirely.

### 3. Protect `POST /run` with a shared secret header

Cloud Scheduler can send a custom header. Add a check:

```
Authorization: Bearer <RUN_SECRET>
```

Store `RUN_SECRET` in Secret Manager and inject as an env var. Requests without the
correct header return `401`. This prevents anyone from triggering a run by guessing the URL.

---

## Step-by-Step Deployment

---

### Step 1 — Prerequisites

```bash
# Install Google Cloud SDK if not already installed
brew install --cask google-cloud-sdk

# Log in
gcloud auth login

# Set the project
gcloud config set project epic-lab-reporter-2026

# Verify billing is enabled
gcloud billing projects describe epic-lab-reporter-2026

# Enable required APIs
gcloud services enable \
  run.googleapis.com \
  secretmanager.googleapis.com \
  cloudbuild.googleapis.com \
  cloudscheduler.googleapis.com \
  artifactregistry.googleapis.com \
  --project=epic-lab-reporter-2026
```

**Verify:** `gcloud services list --enabled` shows all five APIs.

---

### Step 2 — Store RSA Keys in Secret Manager

Keys must never be baked into the Docker image.

```bash
# Store private key
gcloud secrets create epic-private-key \
  --data-file=keys/private.pem \
  --project=epic-lab-reporter-2026

# Store public key
gcloud secrets create epic-public-key \
  --data-file=keys/public.pem \
  --project=epic-lab-reporter-2026

# Store the run trigger secret (generate a strong random value)
echo -n "$(openssl rand -hex 32)" | \
  gcloud secrets create run-secret \
  --data-file=- \
  --project=epic-lab-reporter-2026
```

Grant the default compute service account permission to read secrets:

```bash
PROJECT_NUMBER=$(gcloud projects describe epic-lab-reporter-2026 --format="value(projectNumber)")

gcloud projects add-iam-policy-binding epic-lab-reporter-2026 \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"
```

**Verify:** `gcloud secrets list` shows `epic-private-key`, `epic-public-key`, `run-secret`.

---

### Step 3 — Create Artifact Registry and Build Image

```bash
# Create registry (one-time)
gcloud artifacts repositories create lab-reporter \
  --repository-format=docker \
  --location=us-central1 \
  --project=epic-lab-reporter-2026

# Authenticate Docker to push to the registry
gcloud auth configure-docker us-central1-docker.pkg.dev

# Build and push via Cloud Build (builds in the cloud, no local Docker needed)
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --project=epic-lab-reporter-2026
```

**Verify:** In the GCP Console → Artifact Registry → lab-reporter → you see a tagged image.

---

### Step 4 — Deploy to Cloud Run

Replace placeholder values with your real ones before running.

```bash
gcloud run deploy epic-lab-reporter \
  --image us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --min-instances 1 \
  --max-instances 3 \
  --memory 256Mi \
  --timeout 300 \
  --set-env-vars "\
EPIC_CLIENT_ID=113f4071-f2f7-4c79-a28d-fd8823444f7c,\
EPIC_FHIR_BASE=https://fhir.epic.com/interconnect-fhir-oauth/api/FHIR/R4,\
EPIC_TOKEN_URL=https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token,\
EPIC_GROUP_ID=e3iabhmS8rsueyz7vaimuiaSmfGvi.QwjVXJANlPOgR83,\
EPIC_PRIVATE_KEY_PATH=/app/keys-priv/private.pem,\
EPIC_PUBLIC_KEY_PATH=/app/keys-pub/public.pem,\
SMTP_HOST=smtp.gmail.com,\
SMTP_PORT=587,\
SMTP_USER=philtreffer@gmail.com,\
SMTP_TO=philtreffer@icloud.com,\
SMTP_FROM=Epic Lab Reporter <philtreffer@gmail.com>" \
  --update-secrets "\
SMTP_PASS=smtp-pass:latest,\
RUN_SECRET=run-secret:latest,\
/app/keys-priv/private.pem=epic-private-key:latest,\
/app/keys-pub/public.pem=epic-public-key:latest" \
  --project=epic-lab-reporter-2026
```

> Note: `SMTP_PASS` should also be moved to Secret Manager before deploying.
> Add it: `echo -n "your-app-password" | gcloud secrets create smtp-pass --data-file=- --project=epic-lab-reporter-2026`

Cloud Run will output a permanent URL:
```
Service URL: https://epic-lab-reporter-xxxx-uc.a.run.app
```

**Save this URL — you will use it in Steps 5 and 6.**

**Verify:**
```bash
curl https://epic-lab-reporter-xxxx-uc.a.run.app/.well-known/jwks.json
# Expected: {"keys":[{"kty":"RSA","use":"sig","alg":"RS256",...}]}
```

---

### Step 5 — Update Epic Portal and `.env`

1. Go to [fhir.epic.com](https://fhir.epic.com) → Build Apps → your app
2. Change **Non-Production JWK Set URL** to:
   ```
   https://epic-lab-reporter-xxxx-uc.a.run.app/.well-known/jwks.json
   ```
3. Save the app

Update `EPIC_JWKS_URL` in Cloud Run env vars to match:
```bash
gcloud run services update epic-lab-reporter \
  --update-env-vars EPIC_JWKS_URL=https://epic-lab-reporter-xxxx-uc.a.run.app/.well-known/jwks.json \
  --region us-central1 \
  --project=epic-lab-reporter-2026
```

**Verify:** Trigger a manual run and check logs for `JWKS request: GET /.well-known/jwks.json — Epic-Interconnect/...`

---

### Step 6 — Set Up Cloud Scheduler

Cloud Scheduler replaces the internal `time.Ticker`. It calls `POST /run` every 24 hours.

```bash
# Create a service account for Cloud Scheduler to use
gcloud iam service-accounts create lab-reporter-scheduler \
  --display-name="Lab Reporter Scheduler" \
  --project=epic-lab-reporter-2026

# Grant it permission to invoke the Cloud Run service
gcloud run services add-iam-policy-binding epic-lab-reporter \
  --region=us-central1 \
  --member="serviceAccount:lab-reporter-scheduler@epic-lab-reporter-2026.iam.gserviceaccount.com" \
  --role="roles/run.invoker" \
  --project=epic-lab-reporter-2026

# Create the scheduler job — fires every day at 07:00 UTC
gcloud scheduler jobs create http lab-reporter-daily \
  --location=us-central1 \
  --schedule="0 7 * * *" \
  --uri="https://epic-lab-reporter-xxxx-uc.a.run.app/run" \
  --http-method=POST \
  --oidc-service-account-email="lab-reporter-scheduler@epic-lab-reporter-2026.iam.gserviceaccount.com" \
  --project=epic-lab-reporter-2026
```

**Change the cron expression** to your preferred time.
Common examples:
- `0 7 * * *`  → every day at 07:00 UTC
- `0 6 * * 1`  → every Monday at 06:00 UTC
- `0 */12 * * *` → twice a day

**Verify:** Trigger manually and check logs.
```bash
gcloud scheduler jobs run lab-reporter-daily \
  --location=us-central1 \
  --project=epic-lab-reporter-2026

# Watch live logs
gcloud run services logs read epic-lab-reporter \
  --region=us-central1 \
  --project=epic-lab-reporter-2026 \
  --limit=50
```

---

### Step 7 — Final Verification Checklist

```
[ ] curl /.well-known/jwks.json returns valid JSON
[ ] POST /run returns 200 OK
[ ] Cloud Run logs show "JWKS request: ... User-Agent: Epic-Interconnect/..."
[ ] Cloud Run logs show "access token ✓"
[ ] Cloud Run logs show "scheduler: email sent to ..."
[ ] Email arrives in inbox with lab report
[ ] Cloud Scheduler job shows "Success" in last run status
[ ] Epic portal JWKS URL matches the Cloud Run URL
```

---

### Updating the App (re-deploy after code changes)

```bash
# Rebuild and push
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --project=epic-lab-reporter-2026

# Redeploy (Cloud Run picks up the new image automatically)
gcloud run deploy epic-lab-reporter \
  --image us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --region us-central1 \
  --project=epic-lab-reporter-2026
```

---

## Estimated Monthly Cost

| Resource | Config | Est. Cost |
|----------|--------|-----------|
| Cloud Run (min 1 instance, 256Mi) | Always warm | ~$7–12/month |
| Cloud Run (compute per run, 5 min/day) | 30 runs/month | ~$0.01/month |
| Secret Manager | 4 secrets, <10k accesses | ~$0.03/month |
| Artifact Registry | 1 Docker image ~50MB | ~$0.05/month |
| Cloud Scheduler | 1 job | Free (3 free jobs/month) |
| Cloud Build | 1 build per deploy | Free (120 min/day free tier) |
| **Total** | | **~$8–13/month** |

> Removing `--min-instances=1` brings cost to near zero but risks JWKS cold-start failures with Epic.

---

---

## Optional Extension: Incremental Fetch (`_since` / `_lastUpdated`)

### Goal

Instead of fetching every DiagnosticReport on every run, only fetch resources that have changed since the last successful run. Reduces API calls, faster runs, avoids re-sending alerts for old results.

### How it works in FHIR

**Option A — `_lastUpdated` on DiagnosticReport query** (simpler):
```
GET /DiagnosticReport?patient={id}&category=laboratory&_lastUpdated=gt2026-03-28T07:00:00Z
```

**Option B — `_since` on `$export`** (covers patients + reports in one call):
```
GET /Group/{id}/$export?_type=Patient,DiagnosticReport&_since=2026-03-28T07:00:00Z
```

Pass zero value (`time.Time{}`) to skip the filter and fetch all — used on the very first run.

### Storage of last-run timestamp

| Environment | Where to store | Notes |
|-------------|---------------|-------|
| Local | `last_run.txt` in project root | Trivial, persists between runs |
| Cloud Run | Cloud Storage (GCS) bucket | One tiny file, ~$0/month |
| Cloud Run | Firestore document | Already used for subscriptions extension |

### New files needed

```
epic_lab_reporter/
  state/
    state.go   — ReadLastRun() time.Time, WriteLastRun(time.Time)
```

### Integration point

In `scheduler/scheduler.go`, `runOnce()`:
```go
lastRun := state.ReadLastRun()           // zero value on first run
labMap, err := fhir.FetchLabs(cfg, tc, patients, lastRun)
// ... send email ...
state.WriteLastRun(time.Now())
```

`FetchLabs` accepts an optional `since time.Time` and appends `&_lastUpdated=gt{since}` when non-zero.

---

## Optional Extension: Email Subscription Interface

> This is an add-on. The core service works without it.

### Goal

Let users sign up via a web form to receive the lab report email.
Instead of hardcoding `SMTP_TO` in `.env`, the service reads a subscriber list
and sends one email per subscriber (or a single BCC email).

---

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                                                             │
│   Browser                                                   │
│      │                                                      │
│      │  GET /subscribe                                      │
│      ▼                                                      │
│   Cloud Run Service  (same service, new route)             │
│      │                                                      │
│      │  POST /subscribe  { email: "..." }                   │
│      │                                                      │
│      ▼                                                      │
│   Firestore  (subscribers collection)                      │
│      { email: "phil@example.com", subscribedAt: "..." }    │
│      { email: "jane@example.com", subscribedAt: "..." }    │
│                                                             │
│   POST /run  (existing lab job)                            │
│      │                                                      │
│      │  reads subscriber list from Firestore               │
│      │  sends one email per subscriber                     │
│      ▼                                                      │
│   Gmail SMTP                                               │
│                                                             │
└─────────────────────────────────────────────────────────────┘
```

---

### New Routes to Add

| Route | Method | Description |
|-------|--------|-------------|
| `/subscribe` | `GET` | Serves a simple HTML form |
| `/subscribe` | `POST` | Saves email to Firestore, returns confirmation |
| `/unsubscribe` | `GET` | Removes email from Firestore (linked from email footer) |

---

### New Files

```
epic_lab_reporter/
  web/
    subscribe.go      — GET /subscribe (HTML form) + POST /subscribe (save to Firestore)
    unsubscribe.go    — GET /unsubscribe?email=... (remove from Firestore)
  subscribers/
    firestore.go      — ListSubscribers(), AddSubscriber(), RemoveSubscriber()
```

---

### Firestore Setup

```bash
# Enable Firestore
gcloud firestore databases create \
  --location=us-central1 \
  --project=epic-lab-reporter-2026

# Grant Cloud Run service account Firestore access
gcloud projects add-iam-policy-binding epic-lab-reporter-2026 \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/datastore.user"
```

Firestore collection structure:
```
subscribers/
  <auto-id>/
    email:        "phil@example.com"
    subscribedAt: "2026-03-28T07:00:00Z"
    active:       true
```

---

### Email Sending Change

In `scheduler/scheduler.go`, replace:
```go
email.Send(cfg, html)  // sends to cfg.SMTPTo only
```

With:
```go
subs, _ := subscribers.List(ctx)
for _, sub := range subs {
    email.SendTo(cfg, html, sub.Email)
}
```

---

### Subscription Form (HTML)

Simple, no JavaScript required:

```html
<form method="POST" action="/subscribe">
  <h2>Subscribe to Epic Lab Reports</h2>
  <p>Receive a daily email summary of lab results flagged as abnormal.</p>
  <input type="email" name="email" placeholder="your@email.com" required>
  <button type="submit">Subscribe</button>
</form>
```

---

### Unsubscribe Link in Email Footer

Add to the HTML email template:
```html
<a href="https://epic-lab-reporter-xxxx-uc.a.run.app/unsubscribe?email={{.Email}}">
  Unsubscribe
</a>
```

---

### New Dependencies for the Extension

| Package | Purpose |
|---------|---------|
| `cloud.google.com/go/firestore` | Firestore client |
| `google.golang.org/api` | Google API auth |

These are the only two additional packages needed beyond current dependencies.

---

### Implementation Order for the Extension

| Step | File | Milestone |
|------|------|-----------|
| 1 | `subscribers/firestore.go` | `go run .` can write + read a test subscriber from Firestore |
| 2 | `web/subscribe.go` | `GET /subscribe` shows form, `POST /subscribe` saves to Firestore |
| 3 | `web/unsubscribe.go` | `GET /unsubscribe?email=...` removes subscriber |
| 4 | `scheduler/scheduler.go` | reads subscriber list, sends one email per subscriber |
| 5 | `main.go` | register new routes |
| 6 | Deploy | `POST /subscribe` tested end-to-end from browser |
