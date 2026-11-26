"""Deterministic route inventory plus explicit schemas for the incremental API.

Root field inventories accompany endpoint DTO schemas. --check detects drift in
routes, OpenAPI output and frontend HTTP status definitions.
"""
import difflib
import json
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[1]
backend = root / "backend"
folder = backend / "internal/apidoc"
paths = {}


def route_policies():
    """Read the explicit Go registry used by auth and CSRF middleware.

    Reject unsupported expressions instead of silently inventing a policy. This
    inventory already reads literal HandleFunc registrations from Go source.
    """
    source = (backend / "internal/accesspolicy/policy.go").read_text(encoding="utf-8")
    table = re.search(r"var routes = map\[route\]rule\{\n(.*?)\n\}", source, re.S)
    if table is None:
        raise ValueError("Cannot locate the accesspolicy route registry")
    policies = {}
    for line in table[1].splitlines():
        if not line.strip() or line.lstrip().startswith("//"):
            continue
        entry = re.fullmatch(
            r'\s*\{http\.Method([A-Za-z]+), "([^" ]+)"\}:\s*'
            r'\{access: (Admin|Public|Device|Shared)(, bootstrap: true)?\},\s*', line
        )
        if entry is None:
            raise ValueError(f"Unsupported literal in accesspolicy registry: {line.strip()}")
        method, path, access, bootstrap = entry.groups()
        key = (method.upper(), path)
        if key in policies:
            raise ValueError(f"Duplicate route policy: {key}")
        policies[key] = (access, bool(bootstrap))
    return policies


policies = route_policies()
csrf_exempt = {path for (_, path), (access, bootstrap) in policies.items() if access == "Device" or bootstrap}
fields = {
    "/api/v1/device/login": {"deviceNo": "string", "secret": "string", "platform": "string", "protocolVersion": "integer"},
    "/api/v1/device/report": {"positionSource": "string", "positionSampledAt": "string", "positionAccuracy": "number"},
    "/api/v1/device/sos": {"positionSource": "string", "positionSampledAt": "string", "positionAccuracy": "number"},
    "/api/v1/auth/login": {"phone": "string", "password": "string", "otp": "string", "recoveryCode": "string"},
    "/api/v1/security/factor": {"action": "string", "password": "string", "otp": "string"},
    "/api/v1/security/login-audit": {"after": "integer"},
    "/api/v1/notifications/search": {"after": "integer", "familyId": "integer"},
    "/api/v1/rule-policies": {"deviceId": "integer", "revision": "integer", "policy": "object"},
    "/api/v1/device-commands": {"deviceId": "integer", "action": "string", "requestKey": "string", "ttlSeconds": "integer"},
    "/api/v1/device-command-profiles": {"deviceId": "integer", "wireType": "string", "protocol": "string", "enabled": "boolean"},
    "/api/v1/device-commands/search": {"deviceId": "integer", "after": "integer"},
    "/api/v1/health/daily-query": {"oldId": "integer", "source": "string", "sourceDevice": "string", "metric": "string", "semantics": "string", "day": "string", "offsetMinutes": "integer", "days": "integer"},
    "/api/v1/health/latest": {"oldId": "integer"},
    "/api/v1/alert-reviews": {"recordId": "integer", "kind": "string", "verdict": "string", "reason": "string", "expectedReviewId": "integer"},
    "/api/v1/alert-reviews/search": {"entity": "string", "recordId": "integer", "kind": "string", "after": "integer"},
    "/api/v1/care/handover/create": {"toAdminId": "integer", "note": "string"},
    "/api/v1/care/handover/accept": {"handoverId": "integer"},
    "/api/v1/care/handover/cancel": {"handoverId": "integer"},
}
def json_request(schema):
    return {"required": True, "content": {"application/json": {"schema": schema}}}


csrf_parameter = {"$ref": "#/components/parameters/CsrfToken"}
registered_routes = {}
for file in sorted((backend / "internal/handlers").glob("*.go")):
    if file.name.endswith("_test.go"):
        continue
    for method, path in re.findall(r'\.HandleFunc\("(GET|POST|PUT|PATCH|DELETE) ([^" ]+)"', file.read_text(encoding="utf-8-sig")):
        key = (method, path)
        if key in registered_routes:
            # Database and database-independent routers intentionally mount this
            # same desktop shutdown or local logout handler. Reject all other collisions.
            if key not in {("POST", "/api/v1/desktop/exit"), ("POST", "/api/v1/auth/logout")} or registered_routes[key] != file.name:
                raise ValueError(f"Conflicting HTTP route registration: {method} {path} ({registered_routes[key]} and {file.name})")
        else:
            registered_routes[key] = file.name
        access = policies.get((method, path), ("Admin", False))[0]
        operation = {
            "summary": path.removeprefix("/api/v1/"), "deprecated": False, "tags": ["v1"],
            "responses": {"200": {
                "description": "Go JSON response; payload varies by operation.",
                "content": {"application/json": {"schema": {"type": "object", "additionalProperties": True}}},
            }},
        }
        operation["security"] = [] if access in {"Public", "Device"} else [{"sessionCookie": []}]
        if method != "GET":
            operation["parameters"] = [] if path in csrf_exempt else [csrf_parameter]
            schema = {"type": "object", "additionalProperties": True}
            if path in fields:
                schema["properties"] = {key: {"type": kind} for key, kind in fields[path].items()}
                if path in {"/api/v1/device/report", "/api/v1/device/sos"}:
                    schema["properties"]["positionSource"]["enum"] = ["gps", "network", "fused", "manual", "simulated", "unknown"]
                    schema["properties"]["positionSampledAt"]["format"] = "date-time"
                    schema["properties"]["positionAccuracy"]["exclusiveMinimum"] = 0
                # Device fields are incremental; existing telemetry stays an open object.
                schema["additionalProperties"] = path in {"/api/v1/auth/login", "/api/v1/device/login", "/api/v1/device/report", "/api/v1/device/sos"}
            operation["requestBody"] = json_request(schema)
        operation["description"] = f"Implementation: {file.relative_to(root).as_posix()}. " + ("Device body authentication or signed gateway receipt; not anonymous access." if access == "Device" else "Go API DTOs without explicit schemas remain open objects; consult implementation.")
        if path == "/api/v1/device/login":
            schema["required"] = ["deviceNo", "secret"]
            schema["properties"]["secret"]["writeOnly"] = True
            operation["description"] += (
                " HarmonyOS clients send optional platform=harmonyos with protocolVersion=1; "
                "both are required together when platform is supplied. Authenticated success returns "
                "protocolVersion=1 and maxBatchSize=200 plus the existing bindingVersion/owner. "
                "Unsupported platform/version returns 400. Omit platform when platform-specific capability negotiation is not needed. "
                "Invalid credentials return 401 with code=DEVICE_AUTH_FAILED; disabled devices return 403."
            )
        if path == "/api/v1/health/latest":
            schema["properties"]["oldId"]["minimum"] = 1
            operation["description"] = (
                "Latest nondeleted stored sample per exact source/sourceDevice/metric/semantics. "
                "Order by exact sample.start descending, then revision descending. "
                "Intervals retain both endpoints; daily totals are snapshots, not today's aggregate. "
                "FAMILY may omit oldId and is checked against current ownership; ADMIN supplies oldId. "
                "Historical binding/version and sampling time are preserved; no freshness guarantee or cross-source merging."
            )
            sample_properties = {name: {"type": "string"} for name in (
                "recordId", "source", "sourceDevice", "sourceVersion", "metric", "semantics", "unit", "stage", "timeBasis"
            )}
            sample_properties.update({
                "value": {"type": ["number", "null"]},
                "start": {"type": "string", "format": "date-time"},
                "end": {"type": "string", "format": "date-time"},
                "version": {"type": "integer"},
            })
            record_properties = {name: {"type": "integer"} for name in ("oldId", "bindingVersion", "revision")}
            record_properties.update({
                "sampleId": {"type": "string"},
                "deviceId": {"type": ["integer", "null"]},
                "sample": {"type": "object", "additionalProperties": True, "properties": sample_properties},
            })
            operation["responses"]["200"]["content"]["application/json"]["schema"] = {
                "type": "object", "required": ["returnCode", "oldId", "items"], "additionalProperties": False,
                "properties": {
                    "returnCode": {"type": "string", "const": "1111"},
                    "oldId": {"type": "integer"},
                    "items": {"type": "array", "items": {"type": "object", "additionalProperties": False,
                        "required": list(record_properties), "properties": record_properties}},
                },
            }
        paths.setdefault(path, {})[method.lower()] = operation
# Service status is mounted before the database router.
paths['/api/v1/service/status'] = {'get': {
    'summary': 'service/status', 'security': [], 'tags': ['v1'],
    'responses': {'200': {'description': 'Service database status'}},
}}
contracts = json.loads((folder / 'contracts.json').read_text(encoding='utf-8'))
actual = {method.upper() + ' ' + path for path, methods in paths.items() for method in methods}
if set(contracts) != actual:
    raise ValueError(f'Contract inventory differs from routes: {set(contracts) ^ actual}')
error_schema = {
    'type': 'object', 'required': ['returnCode', 'code', 'message'],
    'properties': {'returnCode': {'const': '0000', 'type': 'string'},
        'code': {'type': 'string'}, 'message': {'type': 'string'}, 'reference': {'type': 'string'}},
    'additionalProperties': True,
}
for key, contract in contracts.items():
    method, path = key.split(' ', 1)
    op = paths[path][method.lower()]
    if method != 'GET' and path != '/api/v1/image/saveImage':
        request_schema = op['requestBody']['content']['application/json']['schema']
        for name in contract['request']:
            request_schema.setdefault('properties', {}).setdefault(name, {})
    if path != '/api/v1/image/download':
        response_schema = op['responses']['200'].setdefault('content', {}).setdefault('application/json', {}).setdefault('schema', {'type': 'object', 'additionalProperties': True})
        for name in contract['response']:
            response_schema.setdefault('properties', {}).setdefault(name, {})
        response_schema.setdefault('properties', {})['returnCode'] = {'type': 'string', 'const': '1111'}
        response_schema['required'] = sorted(set(response_schema.get('required', [])) | {'returnCode'})
    elif path == '/api/v1/image/download':
        op['parameters'] = [{'name': 'name', 'in': 'query', 'required': True, 'schema': {'type': 'string'}}]
        op['responses']['200'] = {'description': 'Image bytes', 'content': {'image/*': {'schema': {'type': 'string', 'format': 'binary'}}}}
    if path in {'/api/v1/notices/create', '/api/v1/notices/update', '/api/v1/gallery/replace'}:
        op['requestBody']['content']['application/json']['schema']['properties']['fileListName'] = {'type': 'array', 'items': {'type': 'string'}}
    if path in {'/api/v1/health/alerts/device-counts', '/api/v1/vitals/alerts/device-counts', '/api/v1/activity/alerts/device-counts'}:
        schema = op['responses']['200']['content']['application/json']['schema']
        for name in schema['properties'].keys() - {'returnCode'}:
            schema['properties'][name] = {'type': 'integer', 'minimum': 0}
            schema['required'].append(name)
        schema['required'].sort()
    if path == '/api/v1/image/saveImage':
        op['requestBody'] = {'required': True, 'content': {'multipart/form-data': {'schema': {'type': 'object', 'required': ['file'], 'properties': {'file': {'type': 'string', 'format': 'binary'}}}}}}

# The frontend status fallback is generated from the owning Go definition.
source = (backend / 'internal/response/response.go').read_text(encoding='utf-8')
definitions = {status: {'code': code, 'message': message} for status, code, message in re.findall(r'case (\d+):\s*return "([^"]+)", "([^"]+)"', source)}
status_target = root / 'frontend/src/api/status-errors.json'
status_data = (json.dumps(definitions, ensure_ascii=False, indent=2) + '\n').encode('utf-8')
if '--check' in sys.argv:
    if not status_target.exists() or status_target.read_bytes() != status_data:
        raise SystemExit('Frontend status definitions are stale; run scripts/generate-openapi.py')
else:
    status_target.write_bytes(status_data)
spec = {"openapi": "3.1.0", "info": {"title": "颐和后端 API", "version": (backend / "VERSION").read_text(encoding="utf-8").strip(), "description": "Native Go API v1 route inventory. No Java or legacy HTTP entry points."}, "components": {"securitySchemes": {"sessionCookie": {"type": "apiKey", "in": "cookie", "name": "YIHE_SESSION"}}}, "paths": dict(sorted(paths.items()))}
spec['components']['schemas'] = {'ApiError': error_schema}
spec['components']['responses'] = {'ApiError': {'description': 'API failure; HTTP status identifies the error category', 'content': {'application/json': {'schema': {'$ref': '#/components/schemas/ApiError'}}}}}

# Reference common OpenAPI objects without changing the effective contract of
# any operation. Keep specialized schemas inline.
components = spec["components"]
csrf_header = {
    "name": "X-CSRF-TOKEN", "in": "header", "required": True,
    "schema": {"type": "string"},
}
components["parameters"] = {"CsrfToken": csrf_header}

shared_request_fields = {
    "JsonObject": (),
    "ResidentId": ("oldId",),
    "DeviceId": ("deviceId",),
    "NoticeId": ("textId",),
    "NamePage": ("name", "page", "pageSize"),
}
request_bodies = components["requestBodies"] = {}
for name, keys in shared_request_fields.items():
    schema = {"type": "object", "additionalProperties": True}
    if keys:
        schema["properties"] = dict.fromkeys(keys, {})
    request_bodies[name] = json_request(schema)

responses = components["responses"]
for name, keys in (("Success", ()), ("ItemsSuccess", ("items",))):
    schema = {
        "type": "object", "additionalProperties": True, "required": ["returnCode"],
        "properties": {"returnCode": {"type": "string", "const": "1111"}, **dict.fromkeys(keys, {})},
    }
    responses[name] = {
        "description": "Go JSON response; payload varies by operation.",
        "content": {"application/json": {"schema": schema}},
    }

for methods in paths.values():
    for op in methods.values():
        for name, body in request_bodies.items():
            if op.get("requestBody") == body:
                op["requestBody"] = {"$ref": f"#/components/requestBodies/{name}"}
                break
        for name in ("Success", "ItemsSuccess"):
            if op["responses"]["200"] == responses[name]:
                op["responses"]["200"] = {"$ref": f"#/components/responses/{name}"}
                break

def operation_patch(defaults, operation):
    """Keep only operation-specific values. Null removes an inherited key."""
    patch = {key: None for key in defaults if key not in operation}
    for key, value in operation.items():
        if key not in defaults:
            patch[key] = value
        elif value != defaults[key]:
            base = defaults[key]
            patch[key] = operation_patch(base, value) if isinstance(base, dict) and isinstance(value, dict) else value
    return patch


success_schema = {
    "type": "object", "additionalProperties": True, "required": ["returnCode"],
    "properties": {"returnCode": {"type": "string", "const": "1111"}},
}
operation_defaults = {
    "deprecated": False, "tags": ["v1"], "security": [{"sessionCookie": []}],
    "responses": {"200": {
        "description": "Go JSON response; payload varies by operation.",
        "content": {"application/json": {"schema": success_schema}},
    }},
}
write_defaults = {
    "parameters": [csrf_parameter],
    "requestBody": json_request({"type": "object", "additionalProperties": True}),
}
for path, methods in paths.items():
    for method, operation in methods.items():
        defaults = {**operation_defaults, "summary": path.removeprefix("/api/v1/")}
        if method != "get":
            defaults.update(write_defaults)
        methods[method] = operation_patch(defaults, operation)

# These errors apply to every operation. The field inventories are loaded from
# contracts.json when the server materializes the public OpenAPI document.
operation_defaults["responses"].update(dict.fromkeys(
    ("400", "401", "403", "404", "405", "409", "413", "415", "429", "500", "503"),
    {"$ref": "#/components/responses/ApiError"},
))
spec["operationDefaults"] = operation_defaults
spec["writeDefaults"] = write_defaults

data = (json.dumps(spec, ensure_ascii=False, indent=2) + "\n").encode("utf-8")
target = folder / "openapi-template.json"
if "--check" in sys.argv:
    if not target.exists() or target.read_bytes() != data:
        committed = json.loads(target.read_text(encoding="utf-8")) if target.exists() else {"paths": {}}
        actual_paths = committed.get("paths", {})
        generated_paths = spec["paths"]
        print("committed paths:", len(actual_paths), "generated paths:", len(generated_paths), file=sys.stderr)
        print("committed-only:", sorted(set(actual_paths) - set(generated_paths)), file=sys.stderr)
        print("generated-only:", sorted(set(generated_paths) - set(actual_paths)), file=sys.stderr)
        print("changed values:", [p for p in generated_paths if p in actual_paths and generated_paths[p] != actual_paths[p]][:12], file=sys.stderr)
        expected = data.decode("utf-8").splitlines(keepends=True)
        actual = target.read_text(encoding="utf-8").splitlines(keepends=True) if target.exists() else []
        diff = difflib.unified_diff(actual, expected, fromfile="committed OpenAPI template", tofile="generated Go routes")
        print("".join(list(diff)[:120]), file=sys.stderr)
        raise SystemExit("OpenAPI is stale; run scripts/generate-openapi.py")
else:
    target.write_bytes(data)
print(f"OpenAPI: {len(paths)} paths")
