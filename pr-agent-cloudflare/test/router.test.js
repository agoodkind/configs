import assert from "node:assert/strict";
import test from "node:test";

import { routeRequest } from "../worker/router.js";

function createEnvironment(forwardedRequests) {
  return {
    PR_AGENT: {
      getByName(name) {
        assert.equal(name, "github-app");
        return {
          async fetch(request) {
            forwardedRequests.push(request);
            return new Response("proxied", { status: 202 });
          },
        };
      },
    },
  };
}

test("health does not start PR-Agent", async function () {
  const forwardedRequests = [];
  const response = await routeRequest(
    new Request("https://reviewer.example/health"),
    createEnvironment(forwardedRequests),
  );

  assert.equal(response.status, 200);
  assert.deepEqual(await response.json(), { status: "ok" });
  assert.equal(forwardedRequests.length, 0);
});

test("webhooks reach the canonical PR-Agent container", async function () {
  const forwardedRequests = [];
  const request = new Request(
    "https://reviewer.example/api/v1/github_webhooks",
    { body: "{}", method: "POST" },
  );

  const response = await routeRequest(request, createEnvironment(forwardedRequests));

  assert.equal(response.status, 202);
  assert.equal(forwardedRequests.length, 1);
});
