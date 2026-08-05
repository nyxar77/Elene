# Repository policy

## Protected mainline

`master` accepts changes only through pull requests. The rule applies to
administrators and has no bypass.

A pull request can merge only when:

- the `quality` check succeeds;
- the branch is current with `master`;
- all review conversations are resolved; and
- the pull request is no longer a draft.

Force pushes and branch deletion are blocked. Squash is the only merge method,
which keeps `master` linear. Merged branches are deleted automatically.

Elene currently has one maintainer, so mandatory approval count is zero.
Requiring one would prevent the maintainer from merging their own pull
requests. When a second maintainer joins, raise the requirement to one
approval, dismiss stale approvals, and require approval after the latest push.

## Continuous integration

The `quality` job is the stable required check. It runs for pull requests,
merge groups, and pushes to `master`.

CI must:

- use read-only repository permissions unless a job has a documented reason for more;
- pin every action to a full commit SHA;
- avoid repository secrets in pull request jobs;
- cancel superseded runs on the same branch or pull request;
- have an explicit timeout; and
- run the same project checks required before a local push.

Dependency changes are reviewed for known vulnerabilities. Dependabot checks
Go modules and GitHub Actions weekly. Dependency pull requests follow the same
merge rules as other changes.

## Local gate

Run both commands before every push:

```bash
nix run .#check
nix flake check --accept-flake-config
```

Do not merge around a failing check. Fix the cause or change the rule in a
separate reviewed pull request when the rule itself is wrong.

## Releases

Elene does not have a deployment workflow yet because it does not have a
versioned distributable artifact. Pull request workflows must never publish
releases.

When releases are added, the workflow must run only from version tags created
from protected `master`, repeat the required quality gate, use least-privilege
permissions, and publish checksums with the artifacts. Signing and provenance
must be added before claiming releases are authenticated.
