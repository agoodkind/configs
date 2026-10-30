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
	$(UV_RUN) pytest
