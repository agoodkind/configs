import { Container } from "@cloudflare/containers";

import { routeRequest } from "./router.js";

export class PrAgentContainer extends Container {
  defaultPort = 3000;
  sleepAfter = "1m";
  envVars = {
    CONFIG__FALLBACK_MODELS: "[]",
    CONFIG__MAX_MODEL_TOKENS: "32000",
    CONFIG__MODEL: "gpt-5.4-nano",
    CONFIG__REASONING_EFFORT: "none",
    GITHUB_APP__BOT_USER: "agoodkind-nano-pr-reviewer[bot]",
    GITHUB_APP__HANDLE_PR_ACTIONS: '["opened", "reopened", "ready_for_review"]',
    GITHUB_APP__HANDLE_PUSH_TRIGGER: "true",
    GITHUB_APP__PR_COMMANDS: '["/review"]',
    GITHUB_APP__PUSH_COMMANDS: '["/review"]',
    GITHUB__APP_ID: "4571682",
    GITHUB__DEPLOYMENT_TYPE: "app",
    GITHUB__PRIVATE_KEY: this.env.GITHUB_PRIVATE_KEY,
    GITHUB__WEBHOOK_SECRET: this.env.GITHUB_WEBHOOK_SECRET,
    GUNICORN_WORKERS: "1",
    OPENAI__KEY: this.env.OPENAI_KEY,
  };
}

export default { fetch: routeRequest };
