# AI usage

This service was implemented with Claude Code (Anthropic) from the dev spec,
in numbered, stacked branches (`branch<N>-<feature>`), one pull request per
branch, for the repository owner to review and merge in order. Each PR also went
through an automated `/code-review` pass; simple findings were fixed in the PR
and the rest were left open for the owner.

What the assistant did: wrote the Go code, tests, Dockerfile, Kubernetes
manifests and workflows, and ran the unit tests and manifest validation.

What was not verified in the assistant's sandbox: a real `kind` deployment, a
`docker build` against the public Go module proxy, the GitHub Actions
workflows, and a connection to the live Jetstream endpoint. The binary was
exercised end to end against a local fake Jetstream server.
