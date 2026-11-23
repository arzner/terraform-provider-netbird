# Contributing

This fork is maintained in the open. This page says how a change travels from an
issue to a release, who decides what, and how upstream work is carried over.
Coding conventions, the test rules and the review checklist are in
[AGENTS.md](AGENTS.md); they apply to every contributor.

## From issue to release

1. **Issue.** Every change starts with an issue that says what is wrong or
   missing and why it matters. Bugs name the NetBird version and the steps;
   features name the NetBird API fields or endpoints they use.
2. **Milestone.** Issues the next release will carry go into its milestone
   (`v0.2.0`, ...). Issues without a milestone are backlog.
3. **Pull request.** One pull request per purpose, opened against `main`, with
   `Closes #N` and the PR template filled in. Keep it small; when a change cannot
   be small, agree the split in the issue first. Push fixes as new commits rather
   than rewriting a branch under review.
4. **CI.** The pull request must pass the required checks: build, `make generate`
   without a diff, lint, unit and integration tests, and the acceptance suite
   with Terraform and OpenTofu against the pinned NetBird version. The suite also
   runs against the newest release of the previous NetBird minor line; a failure
   there needs a fix or a stated reason before merging (see
   [Compatibility with NetBird](README.md#compatibility-with-netbird)).
5. **Review.** Every pull request gets an independent review against the
   [review checklist](AGENTS.md#review-checklist) and is updated until no
   blocking finding is left. A finding is either fixed or answered with why it
   does not apply; problems that belong elsewhere become their own issues.
6. **Merge.** The owner merges with **Create a merge commit**; `main` history is
   never rewritten, and every commit on it is signed. After a merge, the other
   open pull requests are brought up to date by merging `main` into them.
7. **Release.** When a milestone is done, its changelog section
   `## [X.Y.Z] - date` is merged and the owner tags `vX.Y.Z` on `main`. The
   release workflow builds and signs the release and uses that section as the
   notes (see [Maintenance](README.md#maintenance)).

## Roles

- **Owner**: merges pull requests, tags releases, decides issues labelled
  `needs-decision`, and holds the release signing key.
- **Contributors**: open issues and pull requests, keep their pull requests
  green and up to date, and answer review findings. Contributions from anyone
  are welcome.

## Labels

| Label | Meaning |
| --- | --- |
| `bug` | Something does not work as documented. |
| `enhancement` | A new resource, attribute or behaviour. |
| `documentation` | Docs only. |
| `fork-infra` | CI, release, registry and repository process. |
| `upstream` | Tracks or ports an upstream issue or pull request. |
| `needs-decision` | Waits for the owner to choose between options stated in the issue. |
| `blocked-by-server` | Waits for a NetBird server fix. The issue says what to change once the fix ships; it is re-checked on every NetBird version bump and kept open until then. |
| `netbird-latest` | Opened by the daily run when the acceptance suite fails against the newest NetBird release. |

## Upstream

The fork started from
[netbirdio/terraform-provider-netbird](https://github.com/netbirdio/terraform-provider-netbird)
and stays open to it.

- **Merging upstream.** Upstream `main` is merged in through a pull request
  (`git merge upstream/main`), never rebased.
- **Porting an upstream pull request.** Check the fix against the NetBird source
  at the pinned version first; port only what holds. Cherry-pick with
  `git cherry-pick -x` so the commits keep their author and name their origin,
  link the upstream pull request in the issue and the pull request, and credit
  the author in the changelog entry. Changes on top of the port go in separate
  commits.
- **Offering fixes back.** A fix that applies upstream as well is offered there
  as its own pull request against upstream `main`, written for that codebase.

## Commits

- Subjects and pull request titles use `[scope] Subject` (for example
  `[provider] ...`, `[tests] ...`, `[ci] ...`), short and imperative.
- The body gives the problem and the reason for the approach; the diff shows
  the rest.
- Commits are signed; see GitHub's guide to
  [signing commits](https://docs.github.com/en/authentication/managing-commit-signature-verification/signing-commits).
- User-visible changes get an entry in the `Unreleased` section of
  [CHANGELOG.md](CHANGELOG.md).
