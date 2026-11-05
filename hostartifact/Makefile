UV_RUN := uv run --frozen

.PHONY: check fmt format-check lint typecheck ban-object test

check: format-check lint typecheck ban-object test

fmt:
	$(UV_RUN) ruff format
	$(UV_RUN) ruff check --fix

format-check:
	$(UV_RUN) ruff format --check

lint:
	$(UV_RUN) ruff check

typecheck:
	$(UV_RUN) mypy
	$(UV_RUN) basedpyright

ban-object:
	$(UV_RUN) python scripts/ban_object.py src tests scripts

test:
	@set -eu; \
	work=$$(mktemp -d); \
	trap 'rm -rf "$$work"' EXIT; \
	trap 'exit 130' INT; \
	trap 'exit 143' TERM; \
	if [ "$${GITHUB_ACTIONS:-}" = "true" ] && [ -n "$${HOSTARTIFACT_HCL_CHECKER:-}" ]; then \
	  echo "CI must build the native HCL checker from the pinned configsctl launcher" >&2; \
	  exit 1; \
	fi; \
	if [ -n "$${HOSTARTIFACT_HCL_CHECKER:-}" ]; then \
	  checker="$$HOSTARTIFACT_HCL_CHECKER"; \
	  echo "Using the supplied native HCL checker: $$checker"; \
	else \
	  echo "Building the native HCL checker from the pinned configsctl launcher"; \
	  bash scripts/stage-hcl-checker.sh .. "$$work/tools"; \
	  checker="$$work/tools/configsctl"; \
	fi; \
	bash scripts/build-fixture-program.sh "$$work"; \
	HOSTARTIFACT_FIXTURE_PROGRAM_RESULT="$$work/result.json" \
	  HOSTARTIFACT_HCL_CHECKER="$$checker" $(UV_RUN) pytest
