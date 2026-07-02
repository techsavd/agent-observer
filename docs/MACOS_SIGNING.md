# macOS Signing And Notarization

Commercial macOS releases should be signed and notarized before distribution.

## Required Credentials

GitHub or local release environments need:

- `DEVELOPER_ID_APPLICATION`
- `APPLE_ID`
- `APPLE_APP_SPECIFIC_PASSWORD`
- `APPLE_TEAM_ID`

When importing a certificate in CI, also provide:

- `APPLE_DEVELOPER_ID_CERTIFICATE_BASE64`
- `APPLE_DEVELOPER_ID_CERTIFICATE_PASSWORD`

## Local Verification Flow

Build release artifacts:

```bash
make snapshot
```

Sign and submit macOS binaries for notarization:

```bash
DEVELOPER_ID_APPLICATION="Developer ID Application: Example, Inc. (TEAMID)" \
APPLE_ID="release@example.com" \
APPLE_APP_SPECIFIC_PASSWORD="app-specific-password" \
APPLE_TEAM_ID="TEAMID" \
scripts/macos-sign-notarize dist
```

The same helper is available through:

```bash
make sign-macos
```

The helper signs `dist/agent-observer_darwin_*/agent-observer`, verifies code signatures, and submits temporary zip payloads to Apple notarization.

## Release Policy

Unsigned snapshot archives are acceptable for local development only. Official commercial macOS artifacts must be produced from a clean tag, signed, notarized, checksummed, and verified before publication.
