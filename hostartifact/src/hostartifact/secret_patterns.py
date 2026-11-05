from __future__ import annotations

import re

VAULT_PREFIX = "vault_"
VAULT_REFERENCE_PATTERN = re.compile(r"\bvault_[A-Za-z0-9_]+")
TEMPLATE_MARKERS = ("{{", "{%", "{#")
