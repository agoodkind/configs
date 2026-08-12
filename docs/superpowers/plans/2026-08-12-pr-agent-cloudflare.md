# Cloudflare PR Agent Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deploy the personal GitHub App reviewer as a scale-to-zero Cloudflare Container.

**Architecture:** A Worker exposes health and forwards GitHub traffic to one named Container. Worker secrets become Container environment variables.

**Tech Stack:** Cloudflare Workers, Cloudflare Containers, TypeScript, Vitest, PR Agent 0.42.0

## Global Constraints

Secrets must never enter Git, command arguments, or displayed output.

The Cloudflare deployment token must expire within four hours and be revoked after verification.

The Container must use the `basic` instance type and sleep after one idle minute.

---

### Task 1: Worker routing

**Files:**

- Create: `pr-agent-cloudflare/src/index.ts`
- Create: `pr-agent-cloudflare/test/index.test.ts`
- Create: `pr-agent-cloudflare/package.json`
- Create: `pr-agent-cloudflare/vitest.config.ts`

**Interfaces:**

- Consumes: Worker secrets and a `PR_AGENT` Durable Object namespace.
- Produces: `GET /health` and proxied webhook requests.

- [ ] Write a failing Worker boundary test for health and proxy routing.
- [ ] Run `npm test` and confirm the missing Worker fails the test.
- [ ] Implement the Worker and Container class.
- [ ] Run `npm test` and confirm both routes pass.
- [ ] Commit the Worker behavior.

### Task 2: Container deployment

**Files:**

- Create: `pr-agent-cloudflare/Dockerfile`
- Create: `pr-agent-cloudflare/wrangler.jsonc`
- Create: `pr-agent-cloudflare/README.md`
- Create: `pr-agent-cloudflare/Rakefile`

**Interfaces:**

- Consumes: the tested Worker and the pinned PR Agent image.
- Produces: the Cloudflare Worker and `basic` Container deployment.

- [ ] Validate the Wrangler configuration.
- [ ] Add all runtime secrets through standard input.
- [ ] Deploy with the temporary Cloudflare token.
- [ ] Verify `GET /health` and the Container root endpoint.
- [ ] Commit the deployment configuration.

### Task 3: GitHub cutover

**Files:**

- Modify: the GitHub App webhook configuration through GitHub's API.

**Interfaces:**

- Consumes: the deployed Worker URL and GitHub App private key.
- Produces: live webhook delivery to Cloudflare.

- [ ] Change the webhook URL to the deployed Worker endpoint.
- [ ] Trigger and verify one signed delivery.
- [ ] Stop the temporary local reviewer.
- [ ] Revoke and delete the temporary Cloudflare token.
- [ ] Record final status and commit identifiers.
