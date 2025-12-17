---
summary: "Release checklist for gogcli (GitHub release + Homebrew tap)"
---

# Releasing `gogcli`

This playbook mirrors the Homebrew + GitHub flow used in `../camsnap`.

When Peter says “release fully” / “release including Homebrew”: do **all** steps below (CI + tag + GitHub release + tap update + Homebrew sanity install).

Assumptions:
- Repo: `steipete/gogcli`
- Tap repo: `../homebrew-tap` (tap: `steipete/tap`)
- Homebrew formula name: `gogcli` (installs the `gog` binary)

## 0) Prereqs
- Clean working tree on `main`.
- Go toolchain installed (Go version comes from `go.mod`).
- `make` works locally.
- Access to the tap repo (e.g. `steipete/homebrew-tap`).

## 1) Verify build is green
```sh
make ci
```

Optional: confirm GitHub Actions is green for the commit you’re tagging:
```sh
gh run list -L 5 --branch main
```

## 2) Update changelog
- Add a new section to `CHANGELOG.md` for the version you’re releasing.

Example heading:
- `## 0.1.0 - 2025-12-12`

## 3) Tag & push
```sh
git checkout main
git pull

# commit changelog + any release tweaks
git commit -am "release: vX.Y.Z"

git tag -a vX.Y.Z -m "Release X.Y.Z"
git push origin main --tags
```

## 4) Update (or add) the Homebrew formula
In the tap repo (assumed sibling at `../homebrew-tap`), create/update `Formula/gogcli.rb`.

Recommended formula shape (build-from-source, no binary assets needed):
- `version "X.Y.Z"`
- `url "https://github.com/steipete/gogcli/archive/refs/tags/vX.Y.Z.tar.gz"`
- `sha256 "<sha256>"`
- `depends_on "go" => :build`
- Build:
  - `system "go", "build", *std_go_args(ldflags: "-s -w"), "./cmd/gog"`

Compute the SHA256 for the tag tarball:
```sh
curl -L -o /tmp/gogcli.tar.gz https://github.com/steipete/gogcli/archive/refs/tags/vX.Y.Z.tar.gz
shasum -a 256 /tmp/gogcli.tar.gz
```

Commit + push in the tap repo:
```sh
cd ../homebrew-tap
git add Formula/gogcli.rb
git commit -m "gogcli vX.Y.Z"
git push origin main
```

## 5) Sanity-check install from tap
```sh
brew update
brew uninstall gogcli || true
brew untap steipete/tap || true
brew tap steipete/tap
brew install steipete/tap/gogcli
brew test steipete/tap/gogcli

gog --help
```

## 6) Create GitHub Release
- Create a GitHub Release for tag `vX.Y.Z`.
- Title: `gogcli X.Y.Z` (or `gog X.Y.Z` if you prefer the binary name).
- Body: copy the bullets from `CHANGELOG.md` for that version.

Tip (CLI):
```sh
gh release create vX.Y.Z --title "gogcli X.Y.Z" --notes-file /tmp/gogcli-release.md
```

## Notes
- `gog` currently does not print a version string; use tags + changelog as the source of truth.
- If you later add `gog version`, update this doc to validate `gog version` post-install.
