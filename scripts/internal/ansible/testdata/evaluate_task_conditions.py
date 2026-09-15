#!/usr/bin/env python3
"""Evaluate Ansible task conditions with ansible-core's own templar.

The Go test reads set_fact tasks and condition lists from a real task file and
sends them as JSON on stdin, together with the variables and registered results
those tasks read. This script renders each set_fact in order, with that task's
own vars in scope, the way set_fact does. It then evaluates each named condition
list the way a task's when or changed_when does: every item must be true, and
evaluation stops at the first false item. It prints the verdicts and the rendered
facts as JSON on stdout.
"""

from __future__ import annotations

import json
import sys
from typing import TypedDict

from ansible.parsing.dataloader import DataLoader
from ansible.template import Templar, trust_as_template

type JsonValue = str | int | float | bool | None | list[JsonValue] | dict[str, JsonValue]


class FactTask(TypedDict):
    """One set_fact task: its when list, its task vars, and the facts it sets."""

    when: list[str]
    vars: dict[str, str]
    set_fact: dict[str, str]


class EvaluationRequest(TypedDict):
    """The variables in scope, the set_fact tasks in file order, and the
    condition lists to evaluate after them."""

    variables: dict[str, JsonValue]
    facts: list[FactTask]
    conditions: dict[str, list[str]]


class EvaluationResult(TypedDict):
    """The verdict of each condition list and the value of each rendered fact."""

    conditions: dict[str, bool]
    facts: dict[str, JsonValue]


def all_conditions_true(templar: Templar, conditions: list[str]) -> bool:
    """Evaluate a condition list the way a task does, stopping at the first
    false item."""
    for condition in conditions:
        if not templar.evaluate_conditional(trust_as_template(condition)):
            return False
    return True


def render_fact_task(
    loader: DataLoader, variables: dict[str, JsonValue], fact_task: FactTask
) -> dict[str, JsonValue]:
    """Render one set_fact task's values with its task vars in scope. Every value
    is rendered before any is set, as set_fact does."""
    task_scope: dict[str, JsonValue] = dict(variables)
    for name, template in fact_task["vars"].items():
        task_scope[name] = trust_as_template(template)
    templar = Templar(loader=loader, variables=task_scope)
    rendered: dict[str, JsonValue] = {}
    for name, template in fact_task["set_fact"].items():
        rendered[name] = templar.template(trust_as_template(template))
    return rendered


def evaluate(request: EvaluationRequest) -> EvaluationResult:
    """Render the set_fact tasks in order, then evaluate every condition list."""
    loader = DataLoader()
    variables: dict[str, JsonValue] = dict(request["variables"])
    facts: dict[str, JsonValue] = {}
    for fact_task in request["facts"]:
        when_templar = Templar(loader=loader, variables=variables)
        if not all_conditions_true(when_templar, fact_task["when"]):
            continue
        rendered = render_fact_task(loader, variables, fact_task)
        variables.update(rendered)
        facts.update(rendered)
    templar = Templar(loader=loader, variables=variables)
    verdicts: dict[str, bool] = {}
    for name, conditions in request["conditions"].items():
        verdicts[name] = all_conditions_true(templar, conditions)
    return {"conditions": verdicts, "facts": facts}


def main() -> int:
    request: EvaluationRequest = json.load(sys.stdin)
    json.dump(evaluate(request), sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
