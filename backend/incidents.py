"""Pure incident state-transition rules.

This module owns business-valid transitions. Persistence, history and notification
outbox writes remain in care.py so state rules can be tested independently.
"""

try:
    from .common import CLOSED, require, stamp
except ImportError:
    from common import CLOSED, require, stamp


def apply_transition(event, body, actor, clock, staff_exists):
    """Mutate one incident after validating its transition.

    Returns ``(action, note, notification_cycle_changed)``. The caller persists
    the event and enqueues the new notification cycle atomically in its DB tx.
    """
    action = body.get("action")
    note = body.get("note", "")
    require(isinstance(note, str) and len(note) <= 2000)

    if action == "claim":
        require(event["state"] == "new" and event["owner"] is None, 409, "CONFLICT")
        event["owner"] = actor
        event["state"] = "claimed"
        event["notification"]["acknowledgedCycle"] = event["notification"]["cycle"]
        return action, note, False

    require(event["owner"] == actor, 403, "FORBIDDEN")
    if action == "processing":
        require(event["state"] in {"claimed", "escalated"}, 409, "CONFLICT")
        event["state"] = "processing"
        event["notification"]["acknowledgedCycle"] = event["notification"]["cycle"]
        return action, note, False

    require(event["state"] == "processing" and action in CLOSED | {"escalated"}, 409, "CONFLICT")
    require(bool(note.strip()), 422, "NOTE_REQUIRED")
    if action == "escalated":
        target = body.get("targetStaffId")
        require(target != actor and staff_exists(target))
        event["owner"] = target
        event["notification"]["cycle"] += 1
        event["notification"]["deliveredCycle"] = event["notification"]["cycle"]
        event["state"] = action
        return action, note, True

    event["closedAt"] = stamp(clock())
    event["state"] = action
    return action, note, False
