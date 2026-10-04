"""全站布局与可用性巡检（v0.3.0）。
覆盖：居民端 / 登录 / 工作人员端 / 官网，在 6 种屏宽 × 紧凑·标准·特大三档字号下，
检查横向溢出、触控目标 < 44px、桌面侧栏不遮挡内容、首页两列结构、字号控件与持久化、密度联动。
先构建网页：python docs/工具/build_frontend.py；再运行：python tests-added/qa_layout.py"""
import json, os, signal, subprocess, sys, tempfile, time, urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright
from browser_support import launch_chromium

ROOT = Path(__file__).resolve().parents[1]
PORT = int(os.environ.get('YIHE_QA_PORT', '8126'))
DB = os.path.join(tempfile.mkdtemp(prefix='yihe-qa-'), 'qa.sqlite3')
base = f'http://127.0.0.1:{PORT}'
SEED = """
import sys; sys.path.insert(0, sys.argv[2]); import service
s = service.CareStore(sys.argv[1])
s.add_user('nurse1', 'staff', 'staff-1', '护士甲', 'correct-horse-1')
s.add_resident({'id': 1, 'name': '王秀兰', 'age': 78, 'responsible': 'staff-1', 'contacts': [{'name': '女儿', 'phone': '13800138000'}]})
s.add_user('res1', 'resident', '1', '王秀兰', 'correct-horse-3')
"""
subprocess.check_call([sys.executable, '-c', SEED, DB, str(ROOT / 'backend')])
env = {**os.environ, 'YIHE_DEVICE_KEY': 'devkey'}
proc = subprocess.Popen([sys.executable, str(ROOT / 'backend/service.py'), '--db', DB, 'serve', '--port', str(PORT), '--origin', base,
                         '--insecure-local', '--static', str(ROOT / 'docs/dist')], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, preexec_fn=os.setsid)
for _ in range(80):
    try: urllib.request.urlopen(base + '/login.html'); break
    except Exception: time.sleep(0.25)

def device(body, key):
    req = urllib.request.Request(base + '/api/v1/device/events', data=json.dumps(body).encode(), method='POST',
        headers={'Content-Type': 'application/json', 'Authorization': 'Bearer devkey', 'Idempotency-Key': key})
    urllib.request.urlopen(req)

# 页面上超出视口宽度的可见元素（排除各自可滚动容器内部）
OVER = """() => { const w = document.documentElement.clientWidth;
  const scrollable = e => { for (let p = e.parentElement; p && p !== document.body; p = p.parentElement) { const o = getComputedStyle(p).overflowX; if (o === 'auto' || o === 'scroll') return true; } return false; };
  return [...document.querySelectorAll('body *')].filter(e => { const r = e.getBoundingClientRect(), cs = getComputedStyle(e);
    return r.width > 0 && cs.visibility !== 'hidden' && (r.right > w + 1 || r.left < -1) && !e.closest('dialog:not([open])') && !e.closest('[hidden]') && !scrollable(e) && cs.position !== 'fixed'; })
    .slice(0, 4).map(e => e.tagName + '.' + (e.className || e.id) + ':' + Math.round(e.getBoundingClientRect().right) + '>' + w); }"""
SMALL = """() => [...document.querySelectorAll('a[href],button,input:not([type=hidden]):not([type=checkbox]),select,summary,textarea')].filter(e => {
  const r = e.getBoundingClientRect(), cs = getComputedStyle(e); const z = parseFloat(getComputedStyle(document.body).zoom) || 1;
  return r.width > 0 && cs.visibility !== 'hidden' && !e.closest('[hidden]') && !e.closest('dialog:not([open])') && (r.height / z < 43.5 || r.width / z < 43.5)
    && !(e.tagName === 'A' && e.closest('p,li,.login-footer,footer,.site-footer,.nav-links,.estate-caption,.care-directory,header.site-header')) && !e.classList.contains('skip-link'); })
  .slice(0, 6).map(e => e.tagName + '.' + (e.className || e.id || e.textContent.trim().slice(0, 8)) + ' ' + Math.round(e.getBoundingClientRect().width) + 'x' + Math.round(e.getBoundingClientRect().height)) """
FAILED, CHECKS = [], [0]
def check(name, problem):
    CHECKS[0] += 1
    if problem: FAILED.append(f'{name}: {problem}'); print('FAIL', name, problem)

SCALES = (0.9, 1, 1.3)
SIZES = [(320, 640), (360, 740), (390, 844), (768, 1024), (1024, 768), (1280, 860)]
try:
    device({'residentId': 1, 'type': 'heartbeat', 'source': 'sim', 'metrics': {'heartRate': 72, 'bloodOxygen': 97, 'temperature': 36.5, 'systolic': 124, 'diastolic': 80},
            'metricStates': {k: 'normal' for k in ('heartRate', 'bloodOxygen', 'temperature', 'systolic', 'diastolic')}}, 'qa-hb')
    device({'residentId': 1, 'type': 'health', 'source': 'sim', 'active': True, 'severity': 'danger', 'detail': '血氧偏低', 'metrics': {'bloodOxygen': 82}, 'metricStates': {'bloodOxygen': 'danger'}}, 'qa-1')
    with sync_playwright() as p:
        b = launch_chromium(p)
        for (w, h) in SIZES:
            for scale in SCALES:
                tag = f'{w}px x{scale}'
                ctx = b.new_context(viewport={'width': w, 'height': h}); ctx.add_init_script(f"localStorage.setItem('yihe-text-scale','{scale}')")
                pg = ctx.new_page(); errs = []; pg.on('pageerror', lambda e: errs.append(str(e)))
                # ---- 登录页
                pg.goto(base + '/login.html'); pg.wait_for_selector('#loginSubmit'); pg.wait_for_timeout(150)
                check(f'{tag} login overflow', pg.evaluate(OVER)); check(f'{tag} login small targets', pg.evaluate(SMALL))
                pg.fill('#loginName', 'res1'); pg.fill('#loginPassword', 'correct-horse-3')
                pg.click('.xz-eye'); t1 = pg.get_attribute('#loginPassword', 'type'); pg.click('.xz-eye')
                check(f'{tag} password toggle', None if t1 == 'text' else f'type={t1}')
                pg.click('#loginSubmit'); pg.wait_for_url('**/index.html'); pg.wait_for_selector('#residentVitals .vital-card', state='attached', timeout=8000); pg.wait_for_timeout(600)
                # ---- 居民端：字号控件、密度、持久化
                presets = pg.evaluate("() => [...document.querySelectorAll('.xz-fontctl button')].map(b => [b.textContent, b.dataset.scale, b.getAttribute('aria-pressed')])")
                check(f'{tag} font presets', None if [x[0] for x in presets] == ['紧凑', '标准', '大', '特大'] else presets)
                pressed = [x[1] for x in presets if x[2] == 'true']
                check(f'{tag} stored preset pressed', None if pressed == [str(scale)] else pressed)
                dens = pg.evaluate("() => document.documentElement.dataset.density")
                check(f'{tag} density', None if dens == {0.9: 'compact', 1: 'normal', 1.3: 'roomy'}[scale] else dens)
                for tab in ('home', 'health', 'me'):
                    pg.click(f'[data-tab={tab}]'); pg.wait_for_timeout(200)
                    check(f'{tag} resident/{tab} overflow', pg.evaluate(OVER)); check(f'{tag} resident/{tab} small targets', pg.evaluate(SMALL))
                pg.click('[data-tab=home]'); pg.wait_for_timeout(250)
                if w >= 1024:
                    g = pg.evaluate("""() => { const q = s => document.querySelector(s)?.getBoundingClientRect(); const nav = q('.bottom-nav'), page = q('.page'), main = q('.xz-main'), aside = q('.xz-aside');
                      return {navW: nav.width, navH: nav.height, vh: innerHeight, pageLeft: page.left, mainRight: main.right, asideLeft: aside.left}; }""")
                    check(f'{tag} side rail does not overlap content', None if g['pageLeft'] >= g['navW'] - 1 else g)
                    check(f'{tag} rail spans viewport', None if g['navH'] >= g['vh'] - 1 else g)
                    check(f'{tag} home is two columns', None if g['asideLeft'] >= g['mainRight'] - 1 else g)
                else:
                    g = pg.evaluate("""() => { const first = document.querySelector('.xz-aside .resident-contact-jump').getBoundingClientRect(), health = document.querySelector('.home-health').getBoundingClientRect(); return {jumpTop: first.top, healthTop: health.top}; }""")
                    check(f'{tag} quick call comes before status on phones', None if g['jumpTop'] < g['healthTop'] else g)
                check(f'{tag} resident page errors', errs[:2] or None)
                ctx.close()
        # ---- 偏好持久化：切到"紧凑"后刷新仍保持（不预置存储，模拟真实用户）
        ctx = b.new_context(viewport={'width': 390, 'height': 844}); pg = ctx.new_page()
        pg.goto(base + '/login.html'); pg.fill('#loginName', 'res1'); pg.fill('#loginPassword', 'correct-horse-3'); pg.click('#loginSubmit')
        pg.wait_for_url('**/index.html'); pg.wait_for_selector('.xz-fontctl'); pg.click('.xz-fontctl button:has-text("紧凑")'); pg.reload(); pg.wait_for_selector('.xz-fontctl'); pg.wait_for_timeout(300)
        now = pg.evaluate("() => [document.documentElement.dataset.textScale, document.documentElement.dataset.density]")
        check('compact preset persists across reload', None if now == ['0.9', 'compact'] else now); ctx.close()
        # ---- 工作人员端
        for (w, h) in [(1024, 768), (1280, 860)]:
            for scale in SCALES:
                tag = f'staff {w}px x{scale}'
                ctx = b.new_context(viewport={'width': w, 'height': h}); ctx.add_init_script(f"localStorage.setItem('yihe-text-scale','{scale}')")
                pg = ctx.new_page(); errs = []; pg.on('pageerror', lambda e: errs.append(str(e)))
                pg.goto(base + '/管理端.html'); pg.fill('#adminLoginName', 'nurse1'); pg.fill('#adminLoginPassword', 'correct-horse-1'); pg.click('#adminLoginSubmit')
                pg.locator('#staffApp').wait_for(state='visible', timeout=8000); pg.wait_for_timeout(700)
                pg.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()")
                for view in ('summary', 'alerts', 'residents', 'more'):
                    pg.click(f'[data-view={view}]'); pg.wait_for_timeout(250)
                    check(f'{tag} {view} overflow', pg.evaluate(OVER))
                rail = pg.evaluate("() => { const r = document.querySelector('.console-rail .menu-panel').getBoundingClientRect(); return [Math.round(r.bottom), innerHeight]; }")
                check(f'{tag} rail fills viewport', None if rail[0] >= rail[1] - 2 else rail)
                check(f'{tag} font control in settings', None if pg.locator('.panel .xz-fontctl').count() == 1 else 'missing')
                check(f'{tag} page errors', errs[:2] or None)
                ctx.close()
        # ---- 官网
        for page in ('home', 'about', 'services', 'contact', 'guide', 'privacy'):
            for (w, h) in [(360, 740), (1280, 860)]:
                for scale in SCALES:
                    ctx = b.new_context(viewport={'width': w, 'height': h}); ctx.add_init_script(f"localStorage.setItem('yihe-text-scale','{scale}')")
                    pg = ctx.new_page(); pg.goto(base + f'/{page}.html'); pg.wait_for_timeout(350)
                    check(f'public {page} {w}px x{scale} overflow', pg.evaluate(OVER)); ctx.close()
        b.close()
finally:
    os.killpg(os.getpgid(proc.pid), signal.SIGTERM)

print(f'{CHECKS[0]} checks;', 'RESULT: OK' if not FAILED else 'RESULT: ' + str(len(FAILED)) + ' PROBLEMS')
sys.exit(1 if FAILED else 0)
