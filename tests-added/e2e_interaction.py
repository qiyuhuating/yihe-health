"""交互缺陷回归（真实后端 + 真实构建产物）：编辑弹窗不得遮挡危险提醒；跨标签页登录后提交不得失败。
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
DANGER = {'residentId': 1, 'type': 'health', 'source': 'sim', 'active': True, 'severity': 'danger', 'detail': '血氧偏低 80%', 'metrics': {'bloodOxygen': 80}, 'metricStates': {'bloodOxygen': 'danger'}}
WARN = {**DANGER, 'severity': 'warning', 'source': 'warn1', 'detail': '收缩压偏高', 'metrics': {'systolic': 158}, 'metricStates': {'systolic': 'warning'}}
results = []
def check(name, ok, extra=''):
    results.append(ok); print(('PASS ' if ok else 'FAIL ') + name + (('  -> ' + str(extra)) if extra != '' else ''))
def staff_login(pg, user='nurse1', pw='correct-horse-1'):
    pg.goto(base + '/管理端.html'); pg.wait_for_selector('#adminLoginSubmit'); pg.fill('#adminLoginName', user); pg.fill('#adminLoginPassword', pw); pg.click('#adminLoginSubmit')
    pg.locator('#staffApp').wait_for(state='visible', timeout=8000); pg.wait_for_timeout(700)
def dismiss(pg): pg.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
def top_is(pg, dialog_id):
    """dialog 中心点处最上层元素是否属于该 dialog（即没有被别的弹窗遮挡）"""
    return pg.evaluate("""(id) => { const d = document.getElementById(id); if (!d || !d.open) return false; const r = d.getBoundingClientRect();
      const e = document.elementFromPoint(r.left + r.width / 2, r.top + Math.min(r.height / 2, 60)); return !!e && d.contains(e); }""", dialog_id)
def login_elsewhere(pg, user, pw):
    """模拟"在另一个标签页重新登录"：同一浏览器的会话 Cookie 被替换，旧标签页持有的 CSRF 令牌随之失效。"""
    return pg.evaluate("""async ([u, w]) => {
      const first = await (await fetch('/api/v1/session', {credentials: 'same-origin'})).json();
      const r = await fetch('/api/v1/session', {method: 'POST', credentials: 'same-origin', headers: {'Content-Type': 'application/json', 'X-CSRF-Token': first.csrfToken}, body: JSON.stringify({role: 'staff', username: u, password: w})});
      return r.status; }""", [user, pw])
def poll_now(pg): pg.evaluate("() => window.dispatchEvent(new Event('online'))")
def open_first_event(pg):
    pg.locator('[data-event]').first.click(); pg.locator('#eventDialog[open]').wait_for(timeout=4000)

try:
    with sync_playwright() as p:
        b = launch_chromium(p)

        # ---------- 1. 编辑弹窗不得遮挡危险提醒 ----------
        device(WARN, 'w-1')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); errors = []
        A = ctx.new_page(); A.on('pageerror', lambda e: errors.append(str(e)))
        staff_login(A); dismiss(A); open_first_event(A)
        A.locator('#eventNote').fill('正在复测血压，尚未提交')
        device({**DANGER, 'source': 'danger-new', 'detail': '心率过缓 38'}, 'd-1')
        poll_now(A); A.wait_for_timeout(2500)
        check('danger alert is shown on top of an open edit dialog', A.evaluate("() => document.getElementById('dangerModal').open") and top_is(A, 'dangerModal'),
              A.evaluate("() => document.getElementById('dangerModal').open"))
        check('the unsaved note survives while the alert is shown', A.input_value('#eventNote') == '正在复测血压，尚未提交')
        A.evaluate("() => document.getElementById('dismissNewAlerts').click()"); A.wait_for_timeout(300)
        check('closing the alert returns to the edit dialog, content intact', A.evaluate("() => document.getElementById('eventDialog').open") and A.input_value('#eventNote') == '正在复测血压，尚未提交')
        check('the alert is marked as presented only once shown (no repeat after dismissal)', not A.evaluate("() => document.getElementById('dangerModal').open"))
        ctx.close()

        stop(); start()
        device(WARN, 'w-1')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); A = ctx.new_page()
        staff_login(A); dismiss(A); open_first_event(A)
        device({**WARN, 'source': 'warn2', 'detail': '体温偏高'}, 'w-2'); poll_now(A); A.wait_for_timeout(2500)
        note = A.evaluate("() => [...document.querySelectorAll('.dialog-alert-note')].filter(n => !n.hidden && n.offsetParent).map(n => n.textContent)")
        check('warning-only alerts do not interrupt editing but are announced inside the dialog', (not A.evaluate("() => document.getElementById('dangerModal').open")) and len(note) >= 1, note)
        A.evaluate("() => document.querySelector('#eventDialog [data-close], #eventDialog .dialog-close, #eventDialog button[value=cancel]')?.click()")
        ctx.close()

        # ---------- 2. 跨标签页登录后提交不得失败 ----------
        stop(); start(); device(WARN, 'w-1')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); A = ctx.new_page(); errsA = []; A.on('pageerror', lambda e: errsA.append(str(e)))
        staff_login(A); dismiss(A)
        B = ctx.new_page(); B.goto(base + '/login.html'); check('login in another tab succeeded', login_elsewhere(B, 'nurse1', 'correct-horse-1') == 200)
        A.bring_to_front(); open_first_event(A)
        A.get_by_role('button', name='接单', exact=True).click(); A.wait_for_timeout(1500)
        err = A.evaluate("() => (document.getElementById('eventError')||{}).textContent || ''")
        owner = A.evaluate("async () => (await (await fetch('/api/v1/care/snapshot')).json()).events.map(e => [e.state, e.owner])")
        check('claim succeeds in a tab whose CSRF token was replaced by a login in another tab', owner == [['claimed', 'staff-1']] and not err, (owner, err))
        check('no page errors in the first tab', not errsA, errsA[:2])
        ctx.close()

        # 另一个标签页换成了别的账号：本页不得以新账号的身份继续操作
        stop(); start(); device(WARN, 'w-1')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); A = ctx.new_page()
        staff_login(A); dismiss(A)
        B = ctx.new_page(); B.goto(base + '/login.html'); login_elsewhere(B, 'nurse2', 'correct-horse-2')
        A.bring_to_front(); open_first_event(A)
        A.get_by_role('button', name='接单', exact=True).click(); A.wait_for_timeout(1500)
        owner = A.evaluate("async () => (await (await fetch('/api/v1/care/snapshot')).json()).events.map(e => [e.state, e.owner])")
        locked = A.evaluate("() => !document.getElementById('adminLoginOverlay').hidden")
        check('a different account in another tab never acts under this tab\'s identity', owner == [['new', None]] and locked, (owner, locked))
        ctx.close()
        b.close()
finally:
    stop()
print('\nSUMMARY', sum(results), '/', len(results))
sys.exit(0 if all(results) else 1)
