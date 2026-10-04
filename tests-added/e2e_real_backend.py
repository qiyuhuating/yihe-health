"""真实后端 + 真实构建产物的浏览器端到端测试（v0.2.0）。
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
results = []
def check(name, ok, extra=''):
    results.append(ok); print(('PASS ' if ok else 'FAIL ') + name + (('  -> ' + str(extra)) if extra != '' else ''))
def staff_login(pg, user='nurse1', pw='correct-horse-1'):
    pg.goto(base + '/管理端.html'); pg.wait_for_selector('#adminLoginSubmit'); pg.fill('#adminLoginName', user); pg.fill('#adminLoginPassword', pw); pg.click('#adminLoginSubmit')
    pg.locator('#staffApp').wait_for(state='visible', timeout=8000); pg.wait_for_timeout(700)
def state(pg): return pg.evaluate("() => ({app: !document.getElementById('staffApp').hidden, overlay: !document.getElementById('adminLoginOverlay').hidden, title: document.getElementById('adminLoginTitle').textContent, notice: document.getElementById('shieldNotice').hidden ? '' : document.getElementById('shieldNotice').textContent, docTitle: document.title})")
def advance(ctx, pg, ms, step=15000):
    d = 0
    while d < ms: ctx.clock.run_for(step); d += step; pg.wait_for_timeout(120)
try:
    with sync_playwright() as p:
        b = launch_chromium(p)
        device(DANGER, 'k-1')
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); ctx.clock.install(time=datetime.datetime.now())
        errors = []; A = ctx.new_page(); A.on('pageerror', lambda e: errors.append(str(e)))
        staff_login(A)
        check('staff can log in against the real backend', state(A)['app'])
        A.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
        A.locator('[data-event]').first.click(); A.locator('#eventDialog[open]').wait_for(timeout=4000)
        A.get_by_role('button', name='接单', exact=True).click(); A.wait_for_timeout(500)
        A.locator('#eventBody button:has-text("开始处理")').click(); A.wait_for_timeout(500)
        A.locator('#eventNote').fill('已到房间，复测血氧中'); 
        snaps0 = None
        advance(ctx, A, 16 * 60000)
        s = state(A)
        check('idle 16 min -> shield shown, workspace hidden, nothing revoked', (not s['app']) and s['overlay'] and s['title'] == '页面已锁定', s['title'])
        me = A.evaluate("async () => (await fetch('/api/v1/session')).json()")
        check('server session is still valid while shielded (alerts can keep flowing)', me.get('user') is not None)
        # a second resident-side danger arrives while locked (new event via another source)
        device({**DANGER, 'source': 'sim2', 'detail': '心率过高'}, 'k-2')
        advance(ctx, A, 30000)
        s = state(A)
        check('alert raised while locked is announced on the lock screen without patient data', 'pre' not in s['notice'] and '预警' in s['notice'] and '王秀兰' not in s['notice'] and s['docTitle'].startswith('【'), (s['notice'], s['docTitle']))
        A.fill('#adminLoginPassword', 'correct-horse-1'); A.click('#adminLoginSubmit'); A.locator('#staffApp').wait_for(state='visible', timeout=6000); A.wait_for_timeout(900)
        check('unlock restores the open event dialog and its unsaved note', A.evaluate("() => document.getElementById('eventDialog').open") and A.input_value('#eventNote') == '已到房间，复测血氧中', A.input_value('#eventNote') if A.locator('#eventNote').count() else 'no textarea')
        advance(ctx, A, 16 * 60000); s = state(A); check('locks again after another idle period', not s['app'] and s['overlay'])
        A.reload(); A.wait_for_timeout(1200)
        s = state(A); check('reload while shielded does not reveal the workspace', (not s['app']) and s['overlay'] and s['title'] == '页面已锁定', s['title'])
        A.fill('#adminLoginPassword', 'wrong-password'); A.click('#adminLoginSubmit'); A.wait_for_timeout(600)
        check('wrong password keeps it locked', not state(A)['app'])
        A.fill('#adminLoginPassword', 'correct-horse-1'); A.click('#adminLoginSubmit'); A.locator('#staffApp').wait_for(state='visible', timeout=6000)
        check('correct password unlocks', state(A)['app'] and A.evaluate("() => document.getElementById('adminLoginTitle').textContent") == '工作人员登录')
        check('no page errors so far', not errors, errors[:3])
        ctx.close()

        # two tabs sharing activity
        ctx = b.new_context(viewport={'width': 1280, 'height': 860}); ctx.clock.install(time=datetime.datetime.now())
        T1 = ctx.new_page(); staff_login(T1); T2 = ctx.new_page(); T2.goto(base + '/管理端.html'); T2.locator('#staffApp').wait_for(state='visible', timeout=8000)
        advance(ctx, T2, 10 * 60000); T2.mouse.click(600, 400); T2.keyboard.press('Shift'); advance(ctx, T2, 6 * 60000)
        check('tab B activity keeps tab A unlocked (shared idle clock)', state(T1)['app'] and state(T2)['app'], (state(T1)['app'], state(T2)['app']))
        advance(ctx, T2, 10 * 60000)
        check('both tabs lock once every tab is idle', (not state(T1)['app']) and (not state(T2)['app']))
        T1.fill('#adminLoginPassword', 'correct-horse-1'); T1.click('#adminLoginSubmit'); T1.locator('#staffApp').wait_for(state='visible', timeout=6000)
        advance(ctx, T2, 3000, 1000); T2.wait_for_timeout(800)
        check('unlocking one tab resumes the other without a second password', state(T2)['app'], state(T2))
        ctx.close()

        # escalation alert reaches the new owner (fresh database)
        stop(); start()
        device({**DANGER, 'source': 'sim3', 'detail': '跌倒疑似'}, 'k-3')
        c1 = b.new_context(viewport={'width': 1280, 'height': 860}); c2 = b.new_context(viewport={'width': 1280, 'height': 860})
        N1 = c1.new_page(); staff_login(N1); N2 = c2.new_page(); staff_login(N2, 'nurse2', 'correct-horse-2')
        for pg in (N1, N2): pg.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
        N1.locator('[data-event]').first.click(); N1.locator('#eventDialog[open]').wait_for(timeout=4000)
        N1.get_by_role('button', name='接单', exact=True).click(); N1.wait_for_timeout(600); N1.locator('#eventBody button:has-text("开始处理")').click(); N1.wait_for_timeout(600)
        N1.locator('#eventNote').fill('需要医生复核'); N1.locator('summary:has-text("其他处置选项")').click(); N1.locator('#escalateTo').select_option('staff-2'); N1.get_by_role('button', name='升级给其他人员').click(); N1.wait_for_timeout(800)
        N2.wait_for_timeout(500); N2.evaluate("() => window.dispatchEvent(new Event('online'))"); N2.wait_for_timeout(1500)
        opened = N2.evaluate("() => document.getElementById('dangerModal').open")
        check('escalation pops the alert on the receiving staff member\'s screen', opened, N2.evaluate("() => document.getElementById('newAlertText').textContent"))
        # one-tap claim straight from the alert list
        device({**DANGER, 'source': 'sim4', 'detail': '血压骤升'}, 'k-4')
        N2.evaluate("() => window.dispatchEvent(new Event('online'))"); N2.wait_for_timeout(1500)
        N2.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
        quick = N2.locator('[data-quick-claim]'); check('alert cards offer a one-tap claim button', quick.count() >= 1, quick.count())
        quick.first.click(); N2.wait_for_timeout(1200)
        owners = N2.evaluate("async () => (await (await fetch('/api/v1/care/snapshot')).json()).events.filter(e => e.detail === '血压骤升').map(e => [e.state, e.owner])")
        check('one tap claims the event for the signed-in staff member', owners == [['claimed', 'staff-2']], owners)
        c1.close(); c2.close()

        # resident session expiry: page stays usable after re-login
        rctx = b.new_context(viewport={'width': 390, 'height': 844}); rctx.clock.install(time=datetime.datetime.now())
        R = rctx.new_page(); rerrors = []; R.on('pageerror', lambda e: rerrors.append(str(e)))
        R.goto(base + '/login.html'); R.wait_for_selector('#loginSubmit'); R.fill('#loginName', 'res1'); R.fill('#loginPassword', 'correct-horse-3'); R.click('#loginSubmit')
        R.wait_for_url('**/index.html'); R.wait_for_selector('#residentVitals', state='attached', timeout=8000)
        R.click('[data-tab=me]'); R.fill('#profileName', '草稿改名')
        con = sqlite3.connect(DB); con.execute('DELETE FROM sessions'); con.commit(); con.close()
        advance(rctx, R, 30000)
        dlg = R.locator('dialog[open]'); dlg.wait_for(timeout=5000)
        dlg.locator('input[autocomplete=username]').fill('res1'); dlg.locator('input[type=password]').fill('correct-horse-3'); dlg.locator('button[type=submit]').click(); R.wait_for_timeout(1800)
        st = R.evaluate("() => ({dialog: !!document.querySelector('dialog[open]'), inert: document.querySelector('main').inert, name: document.getElementById('profileName').value})")
        check('resident re-login unlocks the page and keeps the unsaved draft', (not st['dialog']) and (not st['inert']) and st['name'] == '草稿改名', st)
        check('no page errors on the resident side', not rerrors, rerrors[:3])
        rctx.close()
        b.close()
finally:
    stop()
print('\nSUMMARY', sum(results), '/', len(results))
sys.exit(0 if all(results) else 1)
