# Publishing cmaker: GitHub + Homebrew tap setup

Step-by-step instructions to take `cmaker` from "goreleaser config verified
locally" (already done, see ROADMAP.md §8) to "a real `brew install cmaker`
works." Do these once for initial setup; the last section is what you repeat
for every future release.

---

## 0. Prerequisites

- A GitHub account.
- `gh` (GitHub CLI) installed and logged in (`gh auth login`), or just use
  the GitHub web UI — either works, steps below show `gh` since it's
  scriptable.
- `goreleaser` installed locally (already done this session: `go install
  github.com/goreleaser/goreleaser/v2@latest`).
- This directory is already a local git repo with one commit (done this
  session). No remote is configured yet.

---

## 1. Create the main `cmaker` repo on GitHub

```bash
cd /Users/sayandeepgiri/Code/Codexpress/cmaker

# Creates a new repo under your account and adds it as the "origin" remote.
# Drop --public if you want it private for now (you can flip visibility
# later in GitHub's settings).
gh repo create cmaker --public --source=. --remote=origin
```

If you'd rather use the web UI: go to <https://github.com/new>, create an
empty repo named `cmaker` (don't initialize it with a README/license/
.gitignore — you already have all of that locally), then:

```bash
git remote add origin https://github.com/<YOUR_USERNAME>/cmaker.git
```

Push what you've got:

```bash
git push -u origin main
```

(If your default branch is named something other than `main`, e.g.
`master`, substitute accordingly — check with `git branch`.)

---

## 2. Create the `homebrew-tap` repo

Homebrew requires taps to live in a repo literally named `homebrew-<name>`.
For a formula/cask reachable as `brew install <your-username>/tap/cmaker`,
you need a *separate* repo named `homebrew-tap`:

```bash
gh repo create homebrew-tap --public --clone
```

That's it for now — you don't need to put anything in it by hand.
goreleaser will push the generated cask file (`Casks/cmaker.rb`) into it
automatically during release, as long as it has permission (see step 4).

---

## 3. Update the placeholders in `.goreleaser.yaml`

Open `.goreleaser.yaml` and replace every `YOUR_GITHUB_USERNAME` with your
real GitHub username (or org name, if you're publishing under an org):

```yaml
homebrew_casks:
  - name: cmaker
    repository:
      owner: YOUR_GITHUB_USERNAME   # <-- change this
      name: homebrew-tap
    directory: Casks
    homepage: "https://github.com/YOUR_GITHUB_USERNAME/cmaker"  # <-- and this
```

There are two occurrences total (`repository.owner` and `homepage`).

Also update the placeholder remote goreleaser needs for `scm` metadata (you
already added the real one in step 1, so this is just confirming it's not
still pointing at the fake placeholder URL from earlier):

```bash
git remote -v
# should show your real https://github.com/<you>/cmaker.git, not
# https://github.com/YOUR_GITHUB_USERNAME/cmaker.git
```

Commit this change:

```bash
git add .goreleaser.yaml
git commit -m "Point goreleaser config at the real GitHub repo"
git push
```

---

## 4. Create a GitHub token goreleaser can use

goreleaser needs a token with permission to:
- create a release + upload assets on `cmaker`
- push a commit (the generated cask file) to `homebrew-tap`

**Easiest path**: a classic Personal Access Token with the `repo` scope
(covers both, since both repos are yours).

1. Go to <https://github.com/settings/tokens/new>
2. Name it something like `cmaker-goreleaser`
3. Scope: check `repo` (the whole group — this grants access to both repos)
4. Generate, copy the token (you won't see it again)

Export it in your shell (don't commit this anywhere):

```bash
export GITHUB_TOKEN="ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

Add that `export` line to your shell profile (`~/.zshrc` etc.) if you want
it to persist across sessions — just make sure that file isn't checked into
any repo.

---

## 5. Cut your first real release

goreleaser derives the version from a git tag, so tag first:

```bash
git tag -a v0.1.0 -m "cmaker v0.1.0"
git push origin v0.1.0
```

Then run the real release (no `--snapshot`, no `--skip=publish` this time):

```bash
goreleaser release --clean
```

This will:
- build all 6 platform/arch binaries
- create archives + checksums
- create a GitHub Release on `cmaker` with those archives attached
- generate `Casks/cmaker.rb` and push it as a commit to your `homebrew-tap`
  repo

Watch the output — if anything fails (e.g. a permissions error), it'll tell
you which step and why.

---

## 6. Verify it actually works

```bash
brew tap <YOUR_GITHUB_USERNAME>/tap
brew install cmaker
cmaker --version
```

If `brew install` complains about checksum mismatches or a 404, double check:
- the release actually has the archives attached (check the Releases page
  on GitHub)
- the cask file in `homebrew-tap` points at the right tag/URL (goreleaser
  should have gotten this right automatically, but worth a glance if
  something's off)

---

## 7. Every future release (the repeatable part)

Once the above is done once, shipping a new version is just:

```bash
git tag -a v0.2.0 -m "cmaker v0.2.0"
git push origin v0.2.0
GITHUB_TOKEN="ghp_..." goreleaser release --clean
```

Users on the tap get the update via:

```bash
brew update
brew upgrade cmaker
```

### Optional: automate this via GitHub Actions instead of running it locally

If you'd rather not run `goreleaser release` from your own machine every
time, you can add a second workflow (separate from the existing
`.github/workflows/ci.yml`, which only runs tests) that fires on tag push:

```yaml
# .github/workflows/release.yml
name: Release

on:
  push:
    tags:
      - "v*"

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version: "1.25"
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

One catch: the default `GITHUB_TOKEN` GitHub Actions provides is scoped only
to the repo the workflow runs in (`cmaker`), so it **can't** push to
`homebrew-tap`. For the Homebrew cask step to work from Actions, create a
separate PAT (same `repo` scope as step 4), add it as a repo secret (e.g.
`HOMEBREW_TAP_TOKEN` under Settings → Secrets and variables → Actions on
the `cmaker` repo), and reference that instead:

```yaml
        env:
          GITHUB_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}
```

This step is optional — running `goreleaser release` locally works fine too,
it's just a manual step instead of an automatic one.
