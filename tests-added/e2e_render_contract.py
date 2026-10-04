"""Compare private controllers without adding production globals or dependencies."""

import argparse, hashlib, json, re
from pathlib import Path
from playwright.sync_api import sync_playwright
from browser_support import launch_chromium

NOW = 1_790_000_000_000
ROOT = Path(__file__).resolve().parents[1]
FIXTURE = {
    "revision": 17,
    "serverTime": "2026-09-22T14:13:20.000Z",
    "staff": [{"id": "staff-1", "name": "护士<甲>"}, {"id": "staff-2", "name": "护士&乙"}],
    "residents": {},
    "events": [],
    "logs": [],
}


def stamp(delta=0):
    from datetime import datetime, timezone

    return (
        datetime.fromtimestamp((NOW + delta) / 1000, timezone.utc)
        .isoformat(timespec="milliseconds")
        .replace("+00:00", "Z")
    )


for i in range(1, 16):
    FIXTURE["residents"][str(i)] = {
        "id": i,
        "name": f'居民<{i}>&"',
        "age": 70 + i,
        "place": "社区<&>",
        "responsible": "staff-1",
        "revision": 3,
        "pos": {"x": i * 45, "y": i * 36},
        "risk": bool(i % 2),
        "deviceState": "online" if i % 2 else "offline",
        "lastSeen": stamp(-700000),
        "updatedAt": stamp(),
        "locationAt": stamp(-700000),
        "heartRate": 75,
        "bloodOxygen": 96,
        "temperature": 36.5,
        "systolic": 128,
        "diastolic": 78,
        "metricStates": {"heartRate": "normal", "bloodOxygen": "danger"},
        "contacts": [{"name": "家属<&>", "phone": "13800138000"}, {"name": "家属乙", "phone": "invalid"}],
        "trend": [
            {"at": stamp(-60000), "heartRate": 75, "bloodOxygen": 95, "systolic": 126, "diastolic": 78},
            {"at": stamp(), "heartRate": 75, "bloodOxygen": 96, "systolic": 128, "diastolic": 78},
        ],
        "chronic": "慢病<&>",
        "medicalHistory": {"pastHistory": "既往<&>", "allergies": "无", "medications": "已核对"},
        "doses": [
            {
                "id": f"dose-{i}-{j}",
                "name": f"药<&>{j}",
                "dueAt": stamp((j - 1) * 7200000),
                "createdAt": stamp(-900000),
                "source": "已核对",
                "confirmedAt": stamp() if j == 0 else None,
            }
            for j in range(3)
        ],
    }
for i, state in enumerate(["new", "claimed", "processing", "escalated", "resolved", "false_alarm"]):
    FIXTURE["events"].append(
        {
            "id": f"e-{i}",
            "residentId": 1,
            "type": ["health", "medication", "offline", "fence"][i % 4],
            "state": state,
            "owner": None if i == 0 else "staff-1" if i % 2 == 0 else "staff-2",
            "severity": "danger" if i % 2 == 0 else "warning",
            "detail": "合成<&>提醒",
            "revision": 4,
            "createdAt": stamp(-900000),
            "dueAt": stamp((i - 2) * 60000),
            "sourceRecoveredAt": stamp() if i % 2 else None,
            "history": [
                {"action": "发现<&>", "at": stamp(-900000), "actor": "system", "note": "备注<&>"},
                {"action": "核实", "at": stamp(), "actor": "staff-1", "note": "草稿<&>"},
            ],
            "notification": {"cycle": 2, "acknowledgedCycle": 0},
        }
    )
FIXTURE["logs"] = [{"residentId": 1, "actor": "staff-1", "at": stamp(), "action": "更新<&>", "note": "内部<&>"}]
STUBS = """() => {
 window.YIHE_RUNTIME_CONFIG={mode:'http',pollIntervalMs:5000};
 window.escapeHtml=x=>String(x??'').replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
 window.displayName=(n,m)=>m?String(n).slice(0,1)+'**':n;
 window.loadJSON=()=>({});window.showToast=()=>{};window.YiheTheme={set:()=>{},get:()=> 'light'};
 window.__calls=[]; window.__shielded=false;
 window.CareSession={current:{role:'staff'},requireRole:()=>new Promise(()=>{}),isShielded:()=>window.__shielded,shieldNotice:n=>__calls.push(['shieldNotice',n])};
 window.CareTransport={newIdempotencyKey:()=> 'fixed-intent-key'};
 window.AdminLogin={onReady:()=>{}};window.Audit={verify:()=>({ok:true}),all:()=>[]};
 window.CareAPI={mode:'http',KEY:'test',STAFF:[],TYPES:{health:'健康',medication:'用药',offline:'离线',fence:'安全'},STATES:{new:'新预警',claimed:'已接单',processing:'处理中',escalated:'已升级',resolved:'已解决',false_alarm:'误报'},now:()=>1790000000000,isOpen:e=>!['resolved','false_alarm'].includes(e.state),snapshot:async()=>structuredClone(__fixture),init:async()=>structuredClone(__fixture),markPresented:async(...args)=>__calls.push(['presented',...args]),saveResident:async(...args)=>__calls.push(['saveResident',...args]),addDose:async(...args)=>{__calls.push(['addDose',...args]);throw Object.assign(Error('模拟结果未知'),{code:'NETWORK_ERROR'});}};
} """


def html(root, page):
    s = (root / "docs" / page).read_text()
    s = re.sub(r"<script\b[^>]*>[\s\S]*?</script>", "", s)
    s = re.sub(r"<meta[^>]*Content-Security-Policy[^>]*>", "", s, flags=re.I)
    s = re.sub(r"<link[^>]*>", "", s)
    return s


STAFF_MODULES = ["staff-core.js", "staff-events.js", "staff-residents.js", "staff-map.js", "staff-settings.js", "staff-care.js"]
STAFF_API = """()=>{const S=window.StaffConsole;window.__contract={setup:(s,a,m)=>{S.state=s;S.actor=a;S.masked=m;S.mount('currentStaff',S.options(a));S.wire();document.getElementById('staffApp').hidden=false;document.getElementById('staffApp').inert=false;S.render();},render:(...x)=>S.render(...x),navigate:(...x)=>S.navigate(...x),openEvent:(...x)=>S.openEvent(...x),openResident:(...x)=>S.openResident(...x),refresh:(...x)=>S.refresh(...x)};}"""


def instrument(root, filename, api):
    code = (root / "docs/脚本" / filename).read_text()
    end = code.rfind("})();")
    assert end >= 0
    return code[:end] + api + "\n" + code[end:]


def record(page, result, label, ids):
    value = page.evaluate(
        "ids=>Object.fromEntries(ids.map(id=>{const e=document.getElementById(id);return [id,{html:e.innerHTML,value:e.value,open:e.open,hidden:e.hidden}]}))",
        ids,
    )
    result[label] = value


def check_resident_page(browser, root, result, errors):
    context = browser.new_context(locale="zh-CN", timezone_id="UTC")
    page = context.new_page()
    page.clock.install(time=NOW)
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.set_content(html(root, "index.html"))
    page.evaluate(STUBS)
    page.evaluate("f=>{window.__fixture=f;CareAPI.STAFF=f.staff}", FIXTURE)
    page.evaluate((root / "docs/脚本/care-ui.js").read_text())
    page.evaluate("()=>CareUI.poll=()=>{}")
    api = """window.__contract={setup:s=>{id=1;render(s);revision=s.revision},render,renderDoses};"""
    page.evaluate(instrument(root, "resident-care.js", api))
    page.evaluate("f=>__contract.setup(f)", FIXTURE)
    ids = [
        "residentVitals",
        "todayDoses",
        "residentAlerts",
        "homeContacts",
        "residentTrend",
        "recordBody",
        "doseHistory",
        "contactFields",
        "statusTitle",
        "healthSummary",
    ]
    record(page, result, "resident-view", ids)
    page.evaluate(
        '()=>{document.getElementById("profileName").value="草稿<&>";document.getElementById("profileName").dispatchEvent(new Event("input",{bubbles:true}));__contract.render(__fixture)}'
    )
    assert page.input_value("#profileName") == "草稿<&>"
    record(page, result, "resident-draft", ids)
    result["public-keys"] = page.evaluate("()=>({ui:Object.keys(CareUI),app:Object.keys(App)})")
    assert not errors, errors
    context.close()


def check_staff_dialogs(page, result, ids):
    page.evaluate('()=>{document.getElementById("eventDialog").close();__contract.openResident(1)}')
    record(page, result, "resident", ids)
    page.evaluate(
        '()=>{const f=document.getElementById("staffContactForm");f.elements.name.value="未提交<&>";f.dispatchEvent(new Event("input",{bubbles:true}));document.querySelector(".resident-maintenance").open=true;__contract.openResident(1)}'
    )
    record(page, result, "resident-dirty", ids)
    assert page.input_value("#staffContactForm [name=name]") == "未提交<&>"
    page.evaluate('()=>__contract.openResident(1,"staffContactForm")')
    record(page, result, "resident-reload", ids)
    page.evaluate(
        '()=>{document.getElementById("residentDialog").close();__shielded=true;return __contract.refresh(true)}'
    )
    record(page, result, "shielded", ids)
    page.evaluate("()=>{__shielded=false;return __contract.refresh(true)}")
    record(page, result, "notification", ["dangerModal", "newAlertText", "dangerTitle"])
    result["staff-calls"] = page.evaluate("()=>__calls")


def check_staff_views(page, result):
    ids = [
        "workCounts",
        "todoEvents",
        "allEvents",
        "residentList",
        "residentPagination",
        "safetyMap",
        "safetyEvents",
        "operationLog",
        "eventBody",
        "residentBody",
    ]
    for view in ["summary", "alerts", "residents", "map", "more"]:
        page.evaluate("v=>__contract.navigate(v)", view)
        record(page, result, "view-" + view, ids)
    page.evaluate('()=>{document.querySelector("[data-page=next]").click()}')
    record(page, result, "page-next", ids)
    for e in FIXTURE["events"]:
        page.evaluate('id=>__contract.openEvent(id,"处置草稿<&>")', e["id"])
        record(page, result, "event-" + e["state"], ["eventBody"])
    return ids


def run(browser, root):
    result = {}
    context = browser.new_context(locale="zh-CN", timezone_id="UTC")
    page = context.new_page()
    page.clock.install(time=NOW)
    errors = []
    page.on("pageerror", lambda e: errors.append(str(e)))
    page.set_content(html(root, "管理端.html"))
    page.evaluate(STUBS)
    page.evaluate("f=>{window.__fixture=f;CareAPI.STAFF=f.staff;}", FIXTURE)
    page.evaluate((root / "docs/脚本/care-ui.js").read_text())
    page.evaluate("()=>CareUI.poll=()=>{}")
    # 控制台拆为 core/events/residents/map/settings/boot 六个模块（共享 window.StaffConsole），按页面加载顺序依次载入。
    for module in STAFF_MODULES:
        page.evaluate((root / "docs/脚本" / module).read_text())
    page.evaluate(STAFF_API)
    page.evaluate('f=>__contract.setup(f,"staff-1",false)', FIXTURE)
    ids = check_staff_views(page, result)
    check_staff_dialogs(page, result, ids)
    context.close()
    check_resident_page(browser, root, result, errors)
    return result


def digest_results(result):
    return {
        key: hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False).encode()).hexdigest()
        for key, value in result.items()
    }


def main():
    parser = argparse.ArgumentParser(description="Exact rendered HTML/state regression against v0.2.2 reference")
    parser.add_argument("--baseline", type=Path, help="Optionally compare another extracted source tree directly")
    args = parser.parse_args()
    with sync_playwright() as p:
        browser = launch_chromium(p)
        after = run(browser, ROOT)
        if args.baseline:
            before = run(browser, args.baseline.resolve())
            assert before == after, "Baseline HTML/state/API call drift"
        browser.close()
    reference = json.loads((ROOT / "tests-added/fixtures/render-contract.json").read_text())
    assert digest_results(after) == reference["baselineHashes"], "Rendered contract differs from v0.2.2 reference"
    print(f"PASS: {len(after)} controller scenarios preserve exact HTML, state, calls and public keys")


if __name__ == "__main__":
    main()
