# PR-Agent on Cloudflare

This deployment runs the official PR-Agent GitHub App image in one scale-to-zero Cloudflare Container.

Run `npm run check` to test the routing Worker and validate the deployment bundle.

Run `npm run deploy` with `CLOUDFLARE_API_TOKEN` and `CLOUDFLARE_ACCOUNT_ID` set. Add `OPENAI_KEY`, `GITHUB_PRIVATE_KEY`, and `GITHUB_WEBHOOK_SECRET` as Cloudflare Worker secrets before enabling the GitHub webhook.
