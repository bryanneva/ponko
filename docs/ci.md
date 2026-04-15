# CI

GitHub Actions CI is backed by Dagger. The same pipeline can be run locally:

```bash
dagger call ci --source=.
dagger call lint --source=.
dagger call build --source=.
dagger call test --source=.
dagger call e-2-e --source=.
```

The Dagger pipeline builds the React dashboard before Go lint, build, and test
steps because `web/embed.go` embeds `web/dist`.

End-to-end tests start Postgres inside Dagger. Anthropic-dependent E2E tests
skip when `ANTHROPIC_API_KEY` is not set. To run those paths locally:

```bash
dagger call e-2-e --source=. --anthropic-api-key env:ANTHROPIC_API_KEY
```

Pull requests also trigger the homelab Tekton/Smee webhook pipeline, which runs
the same `dagger call ci --source=.` command and reports the
`homelab-ci / dagger` GitHub commit status.
