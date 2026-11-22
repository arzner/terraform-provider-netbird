<!-- Why before what: the problem and the reason for this approach. The diff shows the rest. -->

Refs #

## Test level

<!-- Pick one; see "Review checklist" in AGENTS.md. -->
- [ ] Acceptance test (new or changed resource, data source, attribute, mapping, plan modifier or lifecycle)
- [ ] Unit test (client internals, parsing, helpers)
- [ ] None needed (docs, CI, dependency bump without code change, pure refactor)

## Checklist

- [ ] Schema, names, types and effects checked against the NetBird OpenAPI spec at the pinned version
- [ ] Nothing outside the configuration is changed on the server
- [ ] `make generate` run if a schema description or example changed
- [ ] `CHANGELOG.md` updated for user-visible changes
- [ ] Upstream issue or pull request linked, with authorship kept for ported commits
