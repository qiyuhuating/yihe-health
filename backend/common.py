"""Shared validation, serialization and time helpers for the care backend."""

import datetime as dt
import hashlib
import json
import re

TYPES = {"health", "offline", "fence", "medication"}
CLOSED = {"resolved", "false_alarm"}


def stamp(epoch):
    return dt.datetime.fromtimestamp(epoch, dt.timezone.utc).isoformat().replace("+00:00", "Z")


def epoch(value):
    try:
        if not isinstance(value, str) or not re.search(r"(Z|[+-]\d{2}:\d{2})$", value):
            raise ValueError()
        return dt.datetime.fromisoformat(value.replace("Z", "+00:00")).timestamp()
    except (ValueError, TypeError, OverflowError):
        raise Problem(422, "INVALID_TIME")


class Problem(Exception):
    def __init__(self, status, code):
        self.status, self.code = status, code


def require(ok, status=422, code="VALIDATION_FAILED"):
    if not ok:
        raise Problem(status, code)


def string(value, limit):
    require(isinstance(value, str) and 0 < len(value.strip()) <= limit)
    return value.strip()


def integer(value):
    return type(value) is int and 0 <= value <= 9007199254740991


def digest(value):
    return hashlib.sha256(value.encode()).hexdigest()


def encode(value):
    return json.dumps(value, ensure_ascii=False, separators=(",", ":"), allow_nan=False)
