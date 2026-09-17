#!/usr/bin/env python3
"""Evaluate Ansible task expressions with ansible-core's own templar.

A spec reads set_fact tasks, condition lists, and other task fields from a real
task file and sends them as JSON on stdin, together with the variables and
registered results those tasks read. This script renders each set_fact in
order, with that task's own vars in scope, the way set_fact does, templating
the fact names as well as their values. It then evaluates each named condition
list the way a task's when or changed_when does: every item must be true, and
evaluation stops at the first false item. Last it renders each requested set of
templates against the variables and the facts, with that set's own task vars
in scope. It prints the verdicts, the rendered facts, and the rendered
templates as JSON on stdout.
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


class RenderTask(TypedDict):
    """One set of templates to render after the facts: the task vars in scope
    and the templates, keyed by the name each result is reported under."""

    vars: dict[str, str]
    templates: dict[str, str]


class EvaluationRequest(TypedDict):
    """The variables in scope, the set_fact tasks in file order, the condition
    lists to evaluate after them, and the template sets to render last."""

    variables: dict[str, JsonValue]
    facts: list[FactTask]
    conditions: dict[str, list[str]]
    renders: list[RenderTask]


class EvaluationResult(TypedDict):
    """The verdict of each condition list, the value of each rendered fact, and
    the rendered templates of each requested set."""

    conditions: dict[str, bool]
    facts: dict[str, JsonValue]
    renders: list[dict[str, JsonValue]]


def all_conditions_true(templar: Templar, conditions: list[str]) -> bool:
    """Evaluate a condition list the way a task does, stopping at the first
    false item."""
    for condition in conditions:
        if not templar.evaluate_conditional(trust_as_template(condition)):
            return False
    return True


def render_templates(
    loader: DataLoader,
    variables: dict[str, JsonValue],
    task_vars: dict[str, str],
    templates: dict[str, str],
) -> dict[str, JsonValue]:
    """Render one task's templates with its task vars in scope. Every value is
    rendered before any is reported, as set_fact does, and a templated name
    (a set_fact key that carries an expression) is rendered too."""
    task_scope: dict[str, JsonValue] = dict(variables)
    for name, template in task_vars.items():
        task_scope[name] = trust_as_template(template)
    templar = Templar(loader=loader, variables=task_scope)
    rendered: dict[str, JsonValue] = {}
    for name, template in templates.items():
        rendered_name = str(templar.template(trust_as_template(name)))
        rendered[rendered_name] = templar.template(trust_as_template(template))
    return rendered


def evaluate(request: EvaluationRequest) -> EvaluationResult:
    """Render the set_fact tasks in order, evaluate every condition list, then
    render every requested template set."""
    loader = DataLoader()
    variables: dict[str, JsonValue] = dict(request["variables"])
    facts: dict[str, JsonValue] = {}
    for fact_task in request["facts"]:
        when_templar = Templar(loader=loader, variables=variables)
        if not all_conditions_true(when_templar, fact_task["when"]):
            continue
        rendered = render_templates(loader, variables, fact_task["vars"], fact_task["set_fact"])
        variables.update(rendered)
        facts.update(rendered)
    templar = Templar(loader=loader, variables=variables)
    verdicts: dict[str, bool] = {}
    for name, conditions in request["conditions"].items():
        verdicts[name] = all_conditions_true(templar, conditions)
    renders: list[dict[str, JsonValue]] = []
    for render_task in request["renders"]:
        renders.append(render_templates(loader, variables, render_task["vars"], render_task["templates"]))
    return {"conditions": verdicts, "facts": facts, "renders": renders}


def main() -> int:
    request: EvaluationRequest = json.load(sys.stdin)
    json.dump(evaluate(request), sys.stdout)
    return 0


if __name__ == "__main__":
    sys.exit(main())
