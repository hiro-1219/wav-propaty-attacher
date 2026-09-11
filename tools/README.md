# fpcalc placement

Release packages place the separately licensed Chromaprint helper here:

- Windows: `tools/fpcalc.exe`
- macOS/Linux: `tools/fpcalc`

When no bundled helper exists, the application also searches `PATH`.
Do not commit an arbitrary binary without recording its version, checksum, license texts,
and corresponding source URL in `THIRD_PARTY_NOTICES.md`.
