# Coverage gate and remaining work

CI requires at least **95% statement coverage**, aiming for 100%. It runs:

```sh
go test -race -count=1 -covermode=atomic -coverpkg=./... -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tee coverage.txt | python3 -B .github/scripts/check_coverage.py
```

The workflow uses Bash with `set -euo pipefail`, so either command failing fails
the build. The gate rejects absent, duplicate, malformed and out-of-range totals.
It has no configurable lower threshold and no exclusions. The reported total
uses `go tool cover`'s displayed precision, matching the shared RFC workflow.
CI uploads `coverage.out` and the function report `coverage.txt` as the
`statement-coverage` artifact even when the gate fails, unless the run is
cancelled. A run that fails before producing coverage has no profile to upload.

`-coverpkg=./...` is necessary: the checked wire proofs in `internal/rfctest`
exercise the public library from another package. Their execution must count
toward coverage of the library. The checklist command is still run separately;
statement coverage does not measure RFC conformance or checklist completeness.

## Baseline and gaps

This change introduces the gate; it does **not** claim the current suite reaches
95%. Prior measurements recorded under rest-mail/rest-mail-server#374 were below
95%; the authoritative current measurement is the fresh race-coverage result in
CI. A failing result must be fixed with tests, rather than lowering the gate or
removing packages from instrumentation. No full coverage suite is run locally on
the shared Pi for this workflow change.

The exact remaining uncovered statements must be inventoried from that profile
as coverage work continues. Known areas needing an audit include listener setup
and failure paths in `server.go`, implicit TLS, shutdown/connection registration
races, and defensive or backend-error branches in the session and MIME code.
These are test work, not approved exclusions; protocol gaps remain recorded in
the partial `CHECKLIST.md`.

The checker's `main` calls `os.Exit`, which cannot be exercised in-process.
Its logic is in `run`, exercised by the checker tests; CI also executes the CLI.
This statement remains in the coverage denominator.

## Shared workflow follow-up

The audited go-spf producer at
`7d291c08d310b9c62afc287e75a534a39150fafb` does not pass `-coverpkg=./...`
and uses duration-bounded fuzzing. go-imap keeps its existing workflow and
execution-bounded fuzz runs (see golang/go#75804) until the shared producer can
preserve both. Its fixed 95..100% total-validation rule is reproduced locally
with offline fixtures, including pipeline failure propagation. Coverage and
checklist badges remain a later slice after the gate is green.
