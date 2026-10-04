"""局部更新回归（真实后端 + 真实构建产物）：刷新时列表、卡片和计数原地更新，焦点与输入状态保持。
先构建网页：python docs/工具/build_frontend.py；再运行：python tests-added/e2e_real_backend.py"""
import datetime, json, os, signal, sqlite3, subprocess, sys, tempfile, time, urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright
from browser_support import launch_chromium

ROOT = Path(__file__).resolve().parents[1]
PORT = int(os.environ.get('YIHE_E2E_PORT', '8123'))
TMP = tempfile.mkdtemp(prefix='yihe-e2e-')
DB = os.path.join(TMP, 'e2e.sqlite3')
base = f'http://127.0.0.1:{PORT}'
proc = None
SEED = """
import sys; sys.path.insert(0, sys.argv[2]); import service
s = service.CareStore(sys.argv[1])
s.add_user('nurse1', 'staff', 'staff-1', '护士甲', 'correct-horse-1'); s.add_user('nurse2', 'staff', 'staff-2', '护士乙', 'correct-horse-2')
s.add_resident({'id': 1, 'name': '王秀兰', 'age': 78, 'responsible': 'staff-1', 'contacts': [{'name': '女儿', 'phone': '13800138000'}]})
s.add_user('res1', 'resident', '1', '王秀兰', 'correct-horse-3')
"""
def start():
    global proc
    for suffix in ('', '-wal', '-shm'):
        try: os.remove(DB + suffix)
        except FileNotFoundError: pass
    subprocess.check_call([sys.executable, '-c', SEED, DB, str(ROOT / 'backend')])
    env = {**os.environ, 'YIHE_DEVICE_KEY': 'devkey'}
    proc = subprocess.Popen([sys.executable, str(ROOT / 'backend/service.py'), '--db', DB, 'serve', '--port', str(PORT), '--origin', base, '--insecure-local', '--static', str(ROOT / 'docs/dist')], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, preexec_fn=os.setsid)
    for _ in range(80):
        try: urllib.request.urlopen(base + '/login.html'); return
        except Exception: time.sleep(0.25)
    raise SystemExit('backend did not start')
def stop():
    try: os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
    except Exception: pass
    time.sleep(0.6)
start()
def device(body, key):
    req = urllib.request.Request(base + '/api/v1/device/events', data=json.dumps(body).encode(), method='POST',
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer devkey', 'Idempotency-Key': key})
    return urllib.request.urlopen(req).status
def sig(source, detail, sev='warning'):
    return {'residentId': 1, 'type': 'health', 'source': source, 'active': True, 'severity': sev, 'detail': detail, 'metrics': {'systolic': 150}, 'metricStates': {'systolic': sev}}
results = []
def check(name, ok, extra=''):
    results.append(ok); print(('PASS ' if ok else 'FAIL ') + name + (('  -> ' + str(extra)) if extra != '' else ''))
def staff_login(pg, user='nurse1', pw='correct-horse-1'):
    pg.goto(base + '/管理端.html'); pg.wait_for_selector('#adminLoginSubmit'); pg.fill('#adminLoginName', user); pg.fill('#adminLoginPassword', pw); pg.click('#adminLoginSubmit')
    pg.locator('#staffApp').wait_for(state='visible', timeout=8000); pg.wait_for_timeout(700)
def dismiss(pg): pg.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
def refresh(pg): pg.evaluate("() => window.dispatchEvent(new Event('online'))"); pg.wait_for_timeout(1800)
MARK = "() => { document.querySelectorAll('#todoEvents .care-event, #workCounts button').forEach((n, i) => { n.__mark = 'm' + i + ':' + (n.dataset.count || n.className); }); return document.querySelectorAll('#todoEvents .care-event').length; }"
KEPT = "() => [...document.querySelectorAll('#todoEvents .care-event, #workCounts button')].map(n => !!n.__mark)"
FOCUS = "() => { const a = document.activeElement; return a ? (a.dataset.event || a.dataset.quickClaim || a.dataset.filter || a.id || a.tagName) : null; }"
try:
    with sync_playwright() as p:
        b = launch_chromium(p)
        for i in range(3): device(sig(f'w{i}', f'收缩压偏高 {150 + i}'), f'k{i}')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); A = ctx.new_page(); errs = []; A.on('pageerror', lambda e: errs.append(str(e)))
        staff_login(A); dismiss(A)
        n = A.evaluate(MARK); check('three alert cards rendered', n == 3, n)
        A.locator('#todoEvents .care-event [data-event]').nth(1).focus(); focused = A.evaluate(FOCUS)

        # 1) 新增一条无关预警：旧卡片与计数按钮原地保留，焦点不丢
        device(sig('w9', '体温偏高', 'warning'), 'k9'); refresh(A); dismiss(A)
        kept = A.evaluate(KEPT)
        check('existing cards and count buttons are kept (not rebuilt) when a new alert arrives', kept.count(True) >= 3 + 4 and A.evaluate("() => document.querySelectorAll('#todoEvents .care-event').length") == 4, kept)
        check('keyboard focus stays on the same card button', A.evaluate(FOCUS) == focused, (focused, A.evaluate(FOCUS)))
        counts = A.evaluate("() => [...document.querySelectorAll('#workCounts button')].map(b => b.textContent.replace(/\\s+/g, ' ').trim())")
        check('counts are updated in place', counts[0].startswith('4') or '4' in counts[0], counts)

        # 2) 无变化的刷新：不产生任何 DOM 变更
        A.evaluate("() => { window.__mut = 0; new MutationObserver(r => { window.__mut += r.length; }).observe(document.getElementById('todoEvents'), {subtree: true, childList: true, attributes: true, characterData: true}); }")
        refresh(A); refresh(A)
        check('a refresh with no data change causes no DOM mutations in the list', A.evaluate("() => window.__mut") == 0, A.evaluate("() => window.__mut"))

        # 3) 其中一条变化：只更新那一张卡片
        A.evaluate(MARK)
        device(sig('w0', '收缩压偏高 175（更新）', 'danger'), 'k0b'); refresh(A); dismiss(A)
        kept = A.evaluate(KEPT)
        check('only the changed card is replaced; the others are kept', kept.count(False) <= 1 + 1, kept)
        check('focus is unaffected by an update to a different card', A.evaluate(FOCUS) == focused, A.evaluate(FOCUS))

        # 4) 筛选下拉框：刷新不得重置用户选择，也不得抢走焦点
        A.select_option('#todoFilter', 'unassigned'); A.focus('#todoFilter'); refresh(A)
        check('filter selection and focus survive a refresh', A.input_value('#todoFilter') == 'unassigned' and A.evaluate("() => document.activeElement.id") == 'todoFilter')
        check('no page errors', not errs, errs[:2])
        ctx.close()

        # ---------- 居民端 ----------
        stop(); start()
        device({'residentId': 1, 'type': 'heartbeat', 'source': 'sim', 'metrics': {'heartRate': 72, 'bloodOxygen': 97, 'temperature': 36.5, 'systolic': 124, 'diastolic': 80}, 'metricStates': {k: 'normal' for k in ('heartRate', 'bloodOxygen', 'temperature', 'systolic', 'diastolic')}}, 'hb')
        ctx = b.new_context(viewport={'width': 390, 'height': 844}); R = ctx.new_page()
        R.goto(base + '/login.html'); R.fill('#loginName', 'res1'); R.fill('#loginPassword', 'correct-horse-3'); R.click('#loginSubmit'); R.wait_for_url('**/index.html'); R.wait_for_selector('#residentVitals .vital-card', state='attached'); R.wait_for_timeout(600)
        R.evaluate("() => { document.querySelector('.resident-metrics').open = true; document.querySelectorAll('#residentVitals .vital-card').forEach((n, i) => n.__mark = i); }")
        R.evaluate("() => { window.__mut = 0; new MutationObserver(r => { window.__mut += r.length; }).observe(document.getElementById('residentVitals'), {subtree: true, childList: true, attributes: true, characterData: true}); }")
        device({'residentId': 1, 'type': 'heartbeat', 'source': 'sim', 'metrics': {'heartRate': 72, 'bloodOxygen': 97, 'temperature': 36.5, 'systolic': 124, 'diastolic': 80}, 'metricStates': {k: 'normal' for k in ('heartRate', 'bloodOxygen', 'temperature', 'systolic', 'diastolic')}}, 'hb2')
        R.evaluate("() => window.dispatchEvent(new Event('online'))"); R.wait_for_timeout(1800)
        check('resident vitals are not rebuilt when the values are unchanged', R.evaluate("() => window.__mut") == 0 and R.evaluate("() => [...document.querySelectorAll('#residentVitals .vital-card')].every(n => n.__mark !== undefined)"), R.evaluate("() => window.__mut"))
        check('the opened metrics panel stays open', R.evaluate("() => document.querySelector('.resident-metrics').open"))
        ctx.close()
        b.close()
finally:
    stop()
print('\nSUMMARY', sum(results), '/', len(results))
sys.exit(0 if all(results) else 1)
