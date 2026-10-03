---
name: release-publish
description: ".env template only, git ls-files verification without data/*.key/*.pem/.env/*.log/*.exe/dist/, SECURITY.md disclosure, and README alpha disclaimer for HushDrop."
---

# Release & Publication Protocols

Standard checklist and verification rules prior to tagging and publishing HushDrop releases.

## 1. Secrets & Tracking Cleanliness Audit
- **Git Tracking Verification**:
  ```bash
  # Must return ZERO matches
  git ls-files | grep -E "(\.key|\.pem|\.env$|\.log|data/|certs/|\.exe$|dist/|internal/server/web/assets)"
  ```
- **Environment Files**: Only `.env.example` with non-sensitive defaults is tracked. Real `.env` files are strictly excluded via `.gitignore`.
- **Absolute Paths Check**: Ensure no local absolute paths (`C:\`, `/home/`, `Users\`) exist in codebase or documentation.

## 2. Security Documentation & Disclosures
- **`SECURITY.md`**: Must be present in repository root detailing supported versions, reporting email/links, and cryptographic threat model.
- **`README.md`**: Must feature an Alpha status disclaimer warning users against exposing ports to the public internet and guiding them through TLS fingerprint verification.

## 3. Release Pipeline
1. Run local tests: `go test -v -race ./...` and `go vet ./...`.
2. Run frontend build and type check: `npm run build && tsc`.
3. Synchronize `internal/server/web/` assets from `frontend/dist/`.
4. Compile release binaries into `./dist/`.
5. Tag release: `git tag -a vX.Y.Z -m "Release vX.Y.Z"`.
6. Publish via GitHub CLI: `gh release create vX.Y.Z ./dist/* --title "HushDrop vX.Y.Z" --notes "..."`.
