# Cloudflare PR Agent Design

## Goal

Run the personal GitHub App reviewer on Cloudflare without a persistent local host.

## Architecture

A Cloudflare Worker forwards one fixed GitHub webhook path to one `basic` Container. The Container runs the pinned PR Agent GitHub App image and sleeps after one minute without requests.

Cloudflare Worker secrets provide the OpenAI key, GitHub App private key, and webhook secret. The repository stores no secret values.

## Behavior

`GET /health` returns Worker health without starting the Container. All other requests reach the single named Container instance. PR Agent validates GitHub webhook signatures and handles pull request events.

The GitHub App stays installed on all personal repositories. Its webhook changes from the temporary laptop tunnel to the deployed Worker URL.

## Deployment

Wrangler builds and deploys the Worker and Container. Deployment uses a short-lived Cloudflare token restricted to the account and the permissions required for Workers and Containers.

After deployment verification, the deployment token is revoked and removed. Runtime secrets remain in Cloudflare.

## Verification

Automated tests cover the Worker health endpoint and webhook proxy routing. Deployment verification checks Worker health, Container health, GitHub App configuration, webhook delivery, and temporary token revocation.
