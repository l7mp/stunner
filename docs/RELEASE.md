# How to release a major STUNner version

Let the new version be vX.Y.Z.

From v2 on the public repos only test and lint: they carry the source and a raw Dockerfile, and
build no artifacts. Every official artifact is built by the priv-workspace monorepo
(`l7mp/priv-workspace`, checked out at `/export/l7mp/stunner-priv`), which lays the private
overlays (`overlays/stunner`, `overlays/stunner-gateway-operator`) over the OSS trees:

- the `l7mp/stunnerd` and `l7mp/stunner-gateway-operator` images,
- the `l7mp/icetester` and `l7mp/softphone` images,
- the `stunnerctl` and `softphone` binaries, attached to the `l7mp/stunner` GitHub release,
- the dispatch of the `stunner` chart build to `stunner-helm`.

The auth service still builds and publishes its own image, `stunner-helm` publishes the chart, and
the license manager is a separate service with its own release cycle. The old `stunner-priv` and
`stunner-gateway-operator-priv` repos are archived: there is nothing to rebase.

## The tag rule

A release is the same tag, vX.Y.Z, in three repos:

- `l7mp/stunner` and `l7mp/stunner-gateway-operator`: the tags users see, and the code the release
  is built from (`make update TAG=vX.Y.Z` checks them out, and fails if either is missing),
- `l7mp/priv-workspace`: the overlays the release is built from (`release.yml` and `make release`
  refuse to run anywhere but on this tag).

Tag in that order: the two OSS repos first, then priv-workspace, then run the release. The OSS tags
start no build by themselves any more, so tagging them is safe; only the priv-workspace release
pushes images. A pre-release tag (vX.Y.Z-rc1) pushes its version tag but never `latest`.

## STUNner

- remove the doc disclaimers ("this doc is for the dev version, see RTD for stable docs") from the
  README.md and docs/README.md (the priv-workspace release refuses to build while they are there)
- write release notes for vX.Y.Z
- `git pull`
- `go mod tidy`
- make sure `make generate fmt vet test` passes
- make sure `golangci-lint run` passes
- `git push` if there are local changes
- release vX.Y.Z on github (so that we can publish the release notes and the binaries have a place
  to land)
- wait until the test and lint CI goes green
- restore doc disclaimers after the release is out

## STUNner auth service

- this must finish before the chart is built, otherwise the chart points to a missing image
- `git pull`
- bump `github.com/l7mp/stunner/v2` version to vX.Y.Z in the `go.mod`:
  `GOWORK=off go get github.com/l7mp/stunner/v2@vX.Y.Z && go mod tidy`
- make sure `go test ./... -count 1` passes
- make sure `golangci-lint run` passes
- `git push` to send the updated `go.mod/go.sum` files
- release vX.Y.Z on github, the same version as the main stunner repo (the tag rule: the
  priv-workspace release builds both repos at the same tag)
- wait until the image publish CI goes green

## STUNner gateway operator

- `git pull`
- bump `github.com/l7mp/stunner/v2` version to vX.Y.Z in the `go.mod`:
  `GOWORK=off go get github.com/l7mp/stunner/v2@vX.Y.Z && go mod tidy` (keep the intentional
  `github.com/dprotaso/go-yit` indirect pin, and keep the `replace ... => ../stunner` line
  commented out)
- make sure `make test` passes
- make sure `golangci-lint run` passes
- `git push` to send the updated `go.mod/go.sum` files
- release vX.Y.Z on github, the same version as the main stunner repo (the tag rule: the
  priv-workspace release builds both repos at the same tag)
- wait until the test and lint CI goes green

## STUNner Helm chart

- bump the default image tags in `helm/stunner/values.yaml` to vX.Y.Z: the operator, the dataplane
  and the auth service (a stale default bites: in the v2 e2e run the chart still pointed to the
  1.2.1 auth server, which answered every ICE config request of a v2 operator with HTTP 500)
- regenerate `helm/stunner/crds/stunner-crds.yaml` from the operator's `config/crd/bases` if the
  CRDs changed
- make sure `ct lint --chart-dirs helm --charts helm/stunner` passes
- `git push`
- do not publish by hand: the priv-workspace release dispatches the chart build once the images are
  pushed

## priv-workspace: the official artifacts

- `cd /export/l7mp/stunner-priv && git pull`
- `make update TAG=vX.Y.Z` (pulls the OSS repos at the tag and the license manager, lays the
  overlays over the OSS trees, regenerates `go.mod` and `go.work`; fails if an overlay file shadows
  an OSS file)
- make sure `make test` passes (the OSS tests plus the private ones)
- make sure `make lint` passes
- make sure `make images` builds both images locally
- push your overlay fixes, if any: there is no rebase and no force-push any more, a broken seam is
  fixed with a plain commit to `overlays/`
- tag priv-workspace: `git tag vX.Y.Z && git push origin vX.Y.Z` (after the two OSS tags)
- run the `release.yml` workflow in priv-workspace from the vX.Y.Z tag ("Use workflow from": the
  tag), with vX.Y.Z as input
- if CI cannot do it, release by hand from a workstation logged in to Docker Hub: `git checkout
  vX.Y.Z && make release TAG=vX.Y.Z` (update, test, lint, then `make push`, which prints what it
  pushes and refuses a tag that is not a version); the icetester and softphone images, the binaries and the chart
  dispatch then need the matching `release.yml` jobs or their steps by hand
- the release builds and pushes `l7mp/stunnerd:X.Y.Z`, `l7mp/stunner-gateway-operator:X.Y.Z`,
  `l7mp/icetester:X.Y.Z` and `l7mp/softphone:X.Y.Z` (plus `latest` for a final release), attaches
  the `stunnerctl` and `softphone` binaries to the `l7mp/stunner` vX.Y.Z release, and dispatches the
  `stunner` chart build to `stunner-helm`
- wait until the CI/CD pipeline goes green (may take a while)
- if the binary upload fails with 401/403/404, suspect a stale `GH_TOKEN` in priv-workspace first

The dev path needs no human: a push to OSS main dispatches `build.yml` in priv-workspace, whose
jobs `test-stunner`, `test-operator`, `lint`, `images`, `icetester`, `softphone` and `e2e` rebuild and push the
`:dev` images and run the e2e matrix. A red job means no image was pushed; reproduce it locally
with `make update` and the make target of the same name (`make test-stunner`, `make lint`).

## License manager

- separate service, separate release: only touch it if the release changes the feature catalog or
  the license codec
- bump `github.com/l7mp/stunner/v2` version to vX.Y.Z in the `go.mod` if the catalog changed
  (it only uses the open-source feature catalog, so it builds on its own:
  `GOWORK=off go get github.com/l7mp/stunner/v2@vX.Y.Z && go mod tidy`)
- make sure `make test` passes
- release on github following its own versioning

## Check latest build

- the e2e matrix (`l7mp/e2e-test`) runs against the published artifacts after the image push: make
  sure it is all green, and treat a stale image or chart as a release blocker, not a test bug
- move the `go.mod` pins of the consumers (auth service, operator, e2e-test) to vX.Y.Z if not done
  above
