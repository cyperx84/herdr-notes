# Contributing

Keep the product focused: one scratch note per workspace, not a notebook or
logbook. Open an issue before broadening that boundary.

```sh
go mod download
gofmt -w .
go vet ./...
go test -race -count=1 ./...
go build ./cmd/herdr-notes
```

Tests must not touch a user's real Herdr state. Document platform assumptions;
never claim a platform was exercised when it was only cross-compiled. Commits
should be small and signed off if your organization requires DCO.
