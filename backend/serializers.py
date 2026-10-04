"""Resident snapshot contract. Internal JSON is never a resident response DTO.

Keep the v0.2.1 frontend shape, but expose one synthetic care-team identity.
Every nested object uses an allow-list so new internal fields stay private.
"""

CARE_TEAM_ID = "care-team"
METRICS = ("heartRate", "bloodOxygen", "temperature", "systolic", "diastolic", "bloodSugar")
RESIDENT_FIELDS = (
    "id",
    "name",
    "age",
    "risk",
    "revision",
    "deviceState",
    "lastSeen",
    "updatedAt",
    "locationAt",
    *METRICS,
)
DOSE_FIELDS = ("id", "name", "dueAt", "createdAt", "confirmedAt")
EVENT_FIELDS = (
    "id",
    "residentId",
    "type",
    "severity",
    "detail",
    "state",
    "revision",
    "createdAt",
    "dueAt",
    "updatedAt",
    "closedAt",
    "sourceRecoveredAt",
)
NOTIFICATION_FIELDS = ("cycle", "deliveredCycle", "presentedCycle", "acknowledgedCycle")
HISTORY_ACTIONS = {
    "claim",
    "processing",
    "escalated",
    "resolved",
    "false_alarm",
    "发现危险信号",
    "信号恢复，待人工复核",
    "信号再次异常或恶化，重新提醒",
    "页面提醒已展示",
}


def select(value, fields):
    """Select scalar public fields only; never forward an internal object."""
    return {
        key: value[key]
        for key in fields
        if key in value and isinstance(value[key], (str, int, float, bool, type(None)))
    }


def owner(value):
    return CARE_TEAM_ID if value is not None else None


def resident_view(value):
    result = select(value, RESIDENT_FIELDS)
    result["responsible"] = CARE_TEAM_ID
    result["contacts"] = [select(item, ("name", "phone")) for item in value["contacts"]]
    result["pos"] = select(value["pos"], ("x", "y"))
    result["metricStates"] = select(value.get("metricStates", {}), METRICS)
    result["trend"] = [select(item, ("at", *METRICS)) for item in value["trend"]]
    result["doses"] = [{**select(item, DOSE_FIELDS), "source": "工作人员登记"} for item in value["doses"]]
    return result


def event_view(value):
    result = select(value, EVENT_FIELDS)
    result["owner"] = owner(value["owner"])
    result["notification"] = select(value["notification"], NOTIFICATION_FIELDS)
    result["history"] = [
        {
            **select(item, ("at", "state")),
            "owner": owner(item.get("owner")),
            "actor": "system" if item.get("actor") == "system" else "工作人员",
            "action": item["action"] if item.get("action") in HISTORY_ACTIONS else "状态更新",
            "note": "",
        }
        for item in value["history"]
    ]
    return result


def resident_snapshot(value):
    return {
        "revision": value["revision"],
        "serverTime": value["serverTime"],
        "staff": [{"id": CARE_TEAM_ID, "name": "照护团队"}],
        "residents": {key: resident_view(item) for key, item in value["residents"].items()},
        "events": [event_view(item) for item in value["events"]],
        "logs": [],
    }
