
1 — Store keys in Secret Manager
# Store both keys
gcloud secrets create epic-private-key \
  --data-file=keys/private.pem \
  --project=epic-lab-reporter-2026

gcloud secrets create epic-public-key \
  --data-file=keys/public.pem \
  --project=epic-lab-reporter-2026

2 — Grant Cloud Run access to secrets

# Get the default compute service account email
PROJECT_NUMBER=$(gcloud projects describe epic-lab-reporter-2026 --format="value(projectNumber)")

gcloud projects add-iam-policy-binding epic-lab-reporter-2026 \
  --member="serviceAccount:${PROJECT_NUMBER}-compute@developer.gserviceaccount.com" \
  --role="roles/secretmanager.secretAccessor"

3 — Rebuild & push the image (main.go changed)

gcloud builds submit \
  --tag us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --project=epic-lab-reporter-2026




Step 3b — Deploy to Cloud Run → Register in Epic
This is the manual pivot point. We need a public HTTPS URL so Epic can fetch the JWKS. We deploy to Cloud Run now (even before the full app is done) just to get that URL.

1 — Push image to Artifact Registry
Run these in order:

# Create the registry (one-time)
gcloud artifacts repositories create lab-reporter \
  --repository-format=docker \
  --location=us-central1 \
  --project=epic-lab-reporter-2026

# Authenticate Docker to GCP
gcloud auth configure-docker us-central1-docker.pkg.dev

# Build & push (from inside the epic_lab_reporter/ folder)
gcloud builds submit \
  --tag us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --project=epic-lab-reporter-2026

#Deploy to Cloud Run (JWKS-only for now) 

gcloud run deploy epic-lab-reporter \
  --image us-central1-docker.pkg.dev/epic-lab-reporter-2026/lab-reporter/app \
  --region us-central1 \
  --platform managed \
  --allow-unauthenticated \
  --port 8080 \
  --set-env-vars "EPIC_CLIENT_ID=placeholder,EPIC_FHIR_BASE=https://fhir.epic.com/interconnect-fhir-oauth/api/FHIR/R4,EPIC_TOKEN_URL=https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token,EPIC_PRIVATE_KEY_PATH=/app/keys/private.pem,EPIC_PUBLIC_KEY_PATH=/app/keys/public.pem,SMTP_HOST=smtp.gmail.com,SMTP_USER=placeholder,SMTP_PASS=placeholder,SMTP_TO=placeholder,SMTP_FROM=placeholder" \
  --update-secrets "/app/keys/private.pem=epic-private-key:latest,/app/keys/public.pem=epic-public-key:latest" \
  --project=epic-lab-reporter-2026


Run these in order and paste back any errors. Once it deploys, Cloud Run will give you the public URL and we can verify:

Google Public url: 

https://epic-lab-reporter-305549427570.us-central1.run.app/.well-known/jwks.json

curl https://epic-lab-reporter-305549427570.us-central1.run.app/.well-known/jwks.json
 provides the following JWT JSON: 

{"keys":[{"kty":"RSA","use":"sig","alg":"RS256","kid":"ovKPJmy9hiShPRig2S83T00MHajayaLeKTOFfmD19us","n":"u3qNay-1Uglma6pxiV0lj4IhhsjFRfuDnPPcILkgTD__xiNiTQycI1V69VLil5yeFgXmai2L0kaU7rdjYYcqkLPAncQH6MuOd2irxPTr2XgesRich7MP7UZt9lZN5vfxMgHEwpk18BhcehxuudorNkoWAV_3gdsT_YnQtVfsFscCzYMVyKROZ9YwHL2sITgWc28FsAcTNi8J-tsGxORz093vWsGcfCNpJymJg2qBvX1xWXx4wd8gCVBjfVOlIlhIwr4imKqNaknciyMApbO5OvMoHd7q_lSjSoF5sSyUXcRHeYmvhyC4ZYhsmdJLUTcQWt-f0FHd1keeTMkgu-Ivow","e":"AQAB"}]}


curl -v -X POST https://fhir.epic.com/interconnect-fhir-oauth/oauth2/token \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "grant_type=client_credentials&client_assertion_type=urn%3Aietf%3Aparams%3Aoauth%3Aclient-assertion-type%3Ajwt-bearer&client_assertion=eyJhbGciOiJSUzI1NiIsImtpZCI6Im92S1BKbXk5aGlTaFBSaWcyUzgzVDAwTUhhamF5YUxlS1RPRmZtRDE5dXMiLCJ0eXAiOiJKV1QifQ.eyJhdWQiOiJodHRwczovL2ZoaXIuZXBpYy5jb20vaW50ZXJjb25uZWN0LWZoaXItb2F1dGgvb2F1dGgyL3Rva2VuIiwiZXhwIjoxNzc0NjM2MTY2LCJpYXQiOjE3NzQ2MzU4NjYsImlzcyI6ImYxMTNmNDA3MS1mMmY3LTRjNzktYTI4ZC1mZDg4MjM0NDRmN2MiLCJqdGkiOiIxOGEwYzVlYjEzNzE1N2M4Iiwic3ViIjoiZjExM2Y0MDcxLWYyZjctNGM3OS1hMjhkLWZkODgyMzQ0NGY3YyJ9.NJQ77fIqZ0cgy1U5m1V0l6GJlRd243jCyXIUND8TbZIciE23XJth_T_dqmDEzL5Kd9Zq0hhXdaQGdW7oXs5YU9dSBFu56p6s25gSPD9EOwIiPSMd6C40Ki66nXNbSxAPhK49fQjZ2eMUVvBhnCCVqHjVWPNapv77PzbQrKUup5JD51SPfguXHB7cqwdp7HdqwsOYTjLnM1WkI52GApXLYAOIAChOaC2cNDk-cXkKXSupWdVIKZA-IiVeVFPaDxxSdKce4RWwr-mSOVKhYzugt54qaRkO3AnULIrjdI7-CnfbX7yXkw6gNR86Dxjhcb89emh4q4k5YNGl9X34E1mKxg&scope=system%2FPatient.read+system%2FDiagnosticReport.read"



echo -n "lkqc thuo bajb dupg" | gcloud secrets versions add smtp-pass \
  --data-file=- \
  --project=epic-lab-reporter-2026

