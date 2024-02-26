# How to release a major STUNner version

Let the new version be vX.Y.Z.

## STUNner

- remove the doc disclaimers ("this doc is for the dev version, see RTD for stable docs") from the
  README.md and docs/README.md
- write release notes for vX.Y.Z
- `git pull`
- `go mod tidy`
- make sure `go test ./...` passes
- make sure `golangci-lint run` passes
- `git push` if there are local changes
- release vX.Y.Z on github (so that we can publish the release notes)
- wait until the CI/CD pipeline goes green (may take a while)

## STUNner gateway operator

- `git pull`
- bump `github.com/l7mp/stunner` version to vX.Y.Z in the `go.mod`
- `go mod tidy`
- make sure `make test` passes
- make sure `golangci-lint run` passes
- `git push` if there are local changes
- release vX.Y.Z (try to have the same version as the main stunner repo, if possible) on github (so
  that we can publish the release notes)

## STUNner auth service

- `git pull`
- bump `github.com/l7mp/stunner` version to vX.Y.Z  in the `go.mod`
- `go mod tidy`
- make sure `go test ./... -count 1` passes
- make sure `golangci-lint run` passes
- `git push` if there are local changes
- release vX.Y.Z (try to have the same version as the main stunner repo, if possible) on github (so
  that we can publish the release notes)

## Check latest build

- ask @Tamas to run the CI/CD for the intergation test 
