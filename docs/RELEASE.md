# Release Process

1. Confirm the working tree only contains intended changes.
2. Run the production verification suite:

   ```bash
   make test
   make race
   make vet
   make build
   ```

3. If GoReleaser is installed locally, validate packaging:

   ```bash
   goreleaser check
   make snapshot
   ```

4. Tag the release:

   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

5. The release workflow publishes checksummed Linux and macOS archives.

6. For commercial macOS distribution, sign and notarize macOS artifacts before publication. See [MACOS_SIGNING.md](MACOS_SIGNING.md).

## Version Metadata

Release builds inject:

- `internal/app.Version`
- `internal/app.Commit`
- `internal/app.BuildDate`

Verify with:

```bash
agent-observer --version
```
