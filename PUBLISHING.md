# Publishing cmaker packs

A **pack** is a reusable bit of C/C++ code - a function, a class, or a
whole directory - published to the cmaker packs registry so it can be
installed into any project with `cmaker install`. This is different from
`cmaker`'s built-in curated registry (`cmaker search`, no `--remote`):
that's a fixed set of well-known libraries (fmt, nlohmann-json, ...)
shipped inside the `cmaker` binary itself; packs are anyone-can-publish,
hosted on a separate service.

**Status:** invite-only proof of concept. There's no self-service
signup - a maintainer adds your GitHub login to an allowlist by hand
before `cmaker login` will work for your account.

## Quick start

```
cmaker new mypack --pack       # scaffolds manifest.yaml + include/mypack.hpp
cd mypack
# ... edit include/mypack.hpp, update manifest.yaml's description ...
cmaker login                   # once - GitHub device-flow login
cmaker publish                 # packages + uploads it
```

Anyone else (once they're allowlisted and logged in) can then run:

```
cmaker install mypack          # latest published version
cmaker install mypack@1.0.0    # a specific version
```

## The manifest (`manifest.yaml`)

```yaml
name: my-json-helpers
version: 1.2.0
description: small JSON parsing helpers
license: MIT
kind: directory        # function | class | directory
authors: [octocat]
dependencies:
  - name: fmt
    source: registry    # resolves via cmaker's own built-in registry
  - name: some-other-pack
    source: pack
    version: 1.0.0       # pack dependencies are pinned to an exact version
placement:
  - src: include/json_helpers.hpp
    dest: include/json_helpers.hpp
notes: optional free text
```

- `name`/`version` must match `^[A-Za-z0-9._-]+$` (no spaces, no `@`).
- `placement` tells `cmaker install` exactly which tarball files go
  where in the installing project. Omit it entirely and the whole
  tarball gets extracted instead, preserving relative paths - the
  simplest option for a self-contained directory.
- A dependency's `source: registry` resolves through the same path
  `cmaker install <name>` already uses for the built-in registry;
  `source: pack` recurses into another pack install, at the exact
  version given (no `^`/`~` ranges in this version of the registry).

## Versioning

Once a version is published, it's **immutable** - republishing the same
`name@version` is rejected (`409 conflict`). Bump the version in
`manifest.yaml` for any change after the first publish. There's no
delete/yank endpoint; a mistake needs a maintainer to remove it by hand.

## Rate limits

Publishing (not installing/searching) is capped at 20 attempts per hour
per account - a basic guard against an accidental script loop, not a
serious abuse-prevention system. A `429` response includes
`"code": "rate_limited"`.

## Errors

Every failed API call returns `{"error": "<message>", "code": "<code>"}`.
`code` is a small, stable set (`bad_request`, `unauthorized`,
`forbidden`, `not_found`, `conflict`, `rate_limited`, `internal_error`) -
`cmaker install`/`publish` show `error`'s message directly; `code` is
there for anything that wants to branch on it programmatically instead
of matching message text.

## See also

- `PACKS_PLAN.md` (gitignored, personal planning notes) - the original
  architecture design this feature was built from.
- `server/README.md` - running/deploying the API server itself.
