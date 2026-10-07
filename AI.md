# AI.md

2026-10-03 · Isaiah

How I used AI agents on the Zenity take-home: the tools, the setup, the prompts behind the key decisions, and where I pushed back. The design phase ran in the claude.ai app and the build phase in a Claude Code cloud session.

## Tools and models

| Tool | Model | Used for |
| --- | --- | --- |
| Claude (claude.ai app, Project "Zenity interview") | Claude Sonnet 5.5 in the latest sessions; confirm earlier ones | Reading the brief, scoping, and the whole design discussion: pods vs. in-code isolation, transport and backpressure, per-handler prototype and production designs, writing and reviewing this spec |
| Claude Code (cloud session "skydra2", claude.ai web app) | Claude Sonnet 5.5, the session's configured model | Building the service from the dev spec: Go code and tests, Dockerfile, Kustomize manifests, CI and release workflows, eleven stacked branches and pull requests, code review with fixes, and DESIGN.md |

## Configuration that shaped the output

- **Project context:** the assignment brief is stored in the Claude Project "Zenity interview", so every chat starts from it.
- **Standing instructions:** I told the agent to discuss prototype and production designs separately, to draft spec content in plain prose without tables, and to land approved content in the spec exactly as I had seen it, organized by pod with a Prototype and a Production subsection each. For the build, I told the agent to work in numbered side branches stacked on each other (`branch<N>-<feature>`), never to push to main, to test as it goes, and to open one pull request per branch for me to review and merge in order.
- **Rules file:** none.
- **MCP servers:** a docs connector, used to keep this file and the dev spec as living documents. In the build session, the GitHub MCP server, used to open pull requests, post review comments and resolve threads. A session server also attached the LinguaNest backend repo so the agent could read how I write pull requests there, and every PR here follows that structure: a plain imperative title with no numbering, then a What section (what changed, as bullets), a Why section (the reason, with links to related PRs), a Judgment calls section where the agent made a decision I might question, a Testing section (what was run, with an explicit "Not verified" call-out for anything it could not check), a Manual one-time prerequisites section where a person must do something by hand, and a closing Merge order section that says where the PR sits in the stack. Branches were pushed with git.
- **Custom skills / system prompts:** my own writing-style skills (dev-spec-declarative-tone and no-ai-writing-tics) applied when I condensed the dev spec into DESIGN.md. In Claude Code I used the built-in /loop and /code-review skills. No custom system prompt.

## Prompts that did the real work

| Decision | Prompt (abridged) | What came back | My call |
| --- | --- | --- | --- |
| Separate pods per path | "Even before we go to a Jetstream or Kafka design, couldn't we have the processors for the different input types be on different Kubernetes pods? ... I'm not crazy about the processors not being fully independent." | Surfaced the middle option I had skipped: an ingester pod streaming to handler pods over gRPC. The alternatives were one Jetstream connection per pod or the single pod. | Adopted separate pods first, then dropped them for the prototype because they were not worth the time there. Pods stay as the production design. |
| Pods versus loops | "It just feels like slightly extra work to have more than one pod ... we're not truly gaining much from putting the different things in different pods." | The brief allows worker goroutines and in-process queues, and the cost of pods was the gRPC plumbing. It named what is lost (process-level isolation and the scale-to-zero demo) and suggested panic recovery and a clean queue seam to compensate. | Adopted one pod with a loop per handler. I also asked whether the brief required the scale-to-zero demo. It didn't; that was the agent's addition, so the demo hook is left undecided. |
| Backpressure and drops | My plan: gRPC streams between pods, with buffers in the gRPC layer and in each consumer that drop under slowdown. "I do like moving the dropping to the ingester and having it in one place." | Two drop points would leave the ingester blind to handler-side drops. Proposed one drop point in the ingester, with handlers that stop reading when full so gRPC flow control pushes pressure back. | Adopted the single drop point. I chose to drop the newest event and count it per path. |
| Reconnect cursor | "Resume at the last `time_us`. We're not trying to recover events so rewinding a little more is just extra unnecessary work." | The agent had laid out rewinding the cursor a few seconds as a safety margin. | Overrode it: resume at the last `time_us` and skip the repeated first event. Recorded in the spec. |
| Follow bursts | "For follow bursts, we would actually want to use the redis counter." | The agent had tied burst detection to the follow table. I corrected it: the table is only needed for who-follows queries. | Counter-based bursts in Redis. Kept the table as a production extension. |
| Review of the finished spec | "Do another review of the spec for yourself and just see if there are any problems with continuity or anything like that. Or logical issues or whatever, things that may not work." | Six findings, including an engagement alert that could be missed when a count jumps past the threshold and a "shed last" claim with no meaning across independent queues. | Approved all six and let the agent fix them in the spec. |
| Branching model | "Work in a side branch ... build the next feature on a new branch that goes on top of the original branch. I will want to merge into main bit by bit myself." Then: "number the branches ... `branch<number starting from 1>-<feature>`". | Eleven stacked branches, one feature each, with one pull request per branch based on the previous branch. Nothing pushed to main. | Adopted. I merge in order and retarget each PR to main myself. I later asked the agent to confirm each PR's base was the branch it was cut from, and it was. I review each PR on its own and make sure I understand what it adds before merging it. |
| Review loop | "Do a /code-review on all of the PRs. For simple comments, address them in the PR and close the comments yourself. For the questionable ones, tell me what you suggest and leave the comment open for me." | 13 low-severity findings across the PRs. 11 were fixed with tests and their threads resolved. 2 were left open: whether the replay-dedupe key should include the operation, and whether non-commit events should be deduped. The first run only reviewed the last commit, so the agent reran it per PR. | Adopted. I made the call on both open threads: I approved adding the operation to the replay-dedupe key, and the other was closed with the agent's recommendation to leave it for the prototype. |
| Status check | "What is the status? Are you like done or... That was kind of quick." | A plain accounting. What was tested, what was not (Docker build, kind deploy, Actions runs, live Jetstream), that its review had been a self-read, and that DESIGN.md was missing and AI.md overclaimed. | Used it as the to-do list for the next rounds. |
| Who verifies | "Are you able to verify the Docker build in your cloud context? Or is this something that I need to do?" | The agent built the real Dockerfile in its sandbox by adding two lines to trust the sandbox's proxy CA, ran the image with a read-only filesystem and no capabilities, and verified the pinned kubeconform. It found the pin needs a newer Go toolchain in CI. | Left only the kind deploy and the Actions runs for me. I then ran the Go app on my machine and deployed it to kind myself. |

## Where the agent helped, where I overrode it

**Helped**

- Surfacing the middle option I had skipped (separate pods over gRPC) and laying out the alternatives for each pod.
- Drafting each pod's prototype and production design for me to approve, and catching continuity problems when reviewing the finished spec.
- Building the service from the spec: the Go project, the per-path queues and handler loops, tests for routing, handler rules, overflow and panic recovery, the Dockerfile, the Kustomize manifests with a one-namespace check, and the README.
- Running the review loop on its own PRs: 13 low-severity findings, 11 fixed with tests and their threads resolved.
- Finding what it could and could not verify in its sandbox, then verifying the Docker build and the kubeconform pin once I asked.

**Overrode or rejected**

- The single-pod, in-process design the agent first proposed. The brief asks for paths that run, fail and scale independently, so I asked for separate pods behind the ingester, then reversed that myself for scope.
- Rewinding the cursor on reconnect. I wanted the plain resume at the last `time_us`, since we are not trying to recover events.
- Tying follow-burst detection to a database table. I wanted a Redis counter, and kept the table only as a production extension.
- Table-style drafts. I asked for prose organized by pod instead.
- The agent's own PR style, with numbered titles and a custom body. I asked for the LinguaNest backend structure instead.
- Editing the dev spec artifact to shorten it. I asked for a separate Markdown copy so the long spec stays as written.

**What I verified myself**

- I questioned the agent's claim about what a delete event carries and had it point to the Jetstream docs, then chose the design so it doesn't depend on fields a delete lacks.
- I read each pod section before approving it, and asked for a final continuity review of the whole spec.
- I asked who verifies the Docker build and the kubeconform pin, and had the agent run both. Its sandbox could not run a real kind cluster or the GitHub Actions workflows, so those stay mine.
- I had the agent build the feature in an agentic loop end to end as a stack of eleven pull requests, each built on top of the one before it. I went through every PR on its own, checked what it added, and made sure I understood all of it before merging and moving to the next.
- Every PR carries a Testing section with "Not verified" call-outs, in the LinguaNest format. As I reviewed the PRs, wherever a description said Claude had not verified something, I verified it myself. That included running the Go application on my machine and running the app on kind on my machine.
