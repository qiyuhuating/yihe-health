"""样式快照：对每个元素记录计算样式与几何尺寸（96 个页面状态：官网 / 登录 / 居民端 / 工作人员端 × 手机·桌面 × 浅色·深色 × 字号档位，含弹窗与错误态）。
改 CSS 前先录制，改完再比较；任何元素的任何记录属性变化都会列出。
  python tests-added/qa_style_snapshot.py --record  tests-added/.style-before.json
  python tests-added/qa_style_snapshot.py --compare tests-added/.style-before.json
先构建网页：python docs/工具/build_frontend.py"""
import json, os, signal, subprocess, sys, tempfile, time, urllib.request
from pathlib import Path
from playwright.sync_api import sync_playwright
from browser_support import launch_chromium

ROOT = Path(__file__).resolve().parents[1]
mode, target = sys.argv[1], sys.argv[2]
PORT = int(os.environ.get('YIHE_SNAP_PORT', '8140')); base = f'http://127.0.0.1:{PORT}'
DB = os.path.join(tempfile.mkdtemp(prefix='yihe-snap-'), 'snap.sqlite3')
SEED = """
import sys; sys.path.insert(0, sys.argv[2]); import service
s = service.CareStore(sys.argv[1])
s.add_user('nurse1', 'staff', 'staff-1', '护士甲', 'correct-horse-1'); s.add_user('nurse2', 'staff', 'staff-2', '护士乙', 'correct-horse-2')
for i, n, a, r in [(1, '王秀兰', 78, 'staff-1'), (2, '李国栋', 82, 'staff-1'), (3, '陈玉梅', 74, 'staff-2'), (4, '张德海', 86, 'staff-2')]:
    s.add_resident({'id': i, 'name': n, 'age': a, 'responsible': r, 'contacts': [{'name': '家属', 'phone': '1380013800%d' % i}]})
s.add_user('res1', 'resident', '1', '王秀兰', 'correct-horse-3')
"""
subprocess.check_call([sys.executable, '-c', SEED, DB, str(ROOT / 'backend')])
proc = subprocess.Popen([sys.executable, str(ROOT / 'backend/service.py'), '--db', DB, 'serve', '--port', str(PORT), '--origin', base, '--insecure-local', '--static', str(ROOT / 'docs/dist')],
                        env={**os.environ, 'YIHE_DEVICE_KEY': 'devkey'}, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, preexec_fn=os.setsid)
for _ in range(80):
    try: urllib.request.urlopen(base + '/login.html'); break
    except Exception: time.sleep(.25)

def post(body, key):
    req = urllib.request.Request(base + '/api/v1/device/events', data=json.dumps(body).encode(), method='POST',
                                 headers={'Content-Type': 'application/json', 'Authorization': 'Bearer devkey', 'Idempotency-Key': key})
    urllib.request.urlopen(req)
class seed_rich:
    @staticmethod
    def events(_):
        ok = lambda m: {k: 'normal' for k in m}
        m = {'heartRate': 72, 'bloodOxygen': 97, 'temperature': 36.5, 'systolic': 124, 'diastolic': 80}
        for i in range(1, 5): post({'residentId': i, 'type': 'heartbeat', 'source': 'sim', 'metrics': m, 'metricStates': ok(m)}, f'hb{i}')
        post({'residentId': 1, 'type': 'health', 'source': 'sim', 'active': True, 'severity': 'danger', 'detail': '血氧偏低 82%', 'metrics': {**m, 'bloodOxygen': 82}, 'metricStates': {**ok(m), 'bloodOxygen': 'danger'}}, 'e1')
        post({'residentId': 2, 'type': 'health', 'source': 'sim', 'active': True, 'severity': 'warning', 'detail': '收缩压偏高 158', 'metrics': {**m, 'systolic': 158}, 'metricStates': {**ok(m), 'systolic': 'warning'}}, 'e2')
        post({'residentId': 3, 'type': 'fence', 'source': 'sim', 'active': True, 'severity': 'danger', 'detail': '离开安全区域', 'metrics': m, 'metricStates': ok(m)}, 'e3')

PROPS = ['display','position','top','left','right','bottom','width','height','min-width','max-width','min-height','margin-top','margin-right','margin-bottom','margin-left','padding-top','padding-right','padding-bottom','padding-left',
 'border-top-width','border-right-width','border-bottom-width','border-left-width','border-top-style','border-top-color','border-bottom-color','border-left-color','border-right-color','border-top-left-radius','border-bottom-right-radius',
 'color','background-color','background-image','box-shadow','opacity','transform','z-index','overflow-x','overflow-y','visibility',
 'font-family','font-size','font-weight','font-style','line-height','letter-spacing','text-align','text-decoration-line','text-transform','white-space',
 'flex-direction','flex-wrap','flex-grow','flex-shrink','flex-basis','align-items','justify-content','gap','grid-template-columns','grid-template-rows','grid-column-start','grid-row-start','order','cursor','outline-style','content','zoom','filter','backdrop-filter']
JS = """(props) => { const out = []; let i = 0;
  const sig = (cs) => props.map(p => cs.getPropertyValue(p)).join('|');
  for (const el of document.querySelectorAll('body, body *')) { const r = el.getBoundingClientRect(); const cs = getComputedStyle(el);
    if (['SCRIPT','STYLE','NOSCRIPT','LINK','META'].includes(el.tagName)) continue;
    let row = [el.tagName + '.' + (el.id || (typeof el.className === 'string' ? el.className.split(' ')[0] : '')), Math.round(r.x), Math.round(r.y), Math.round(r.width), Math.round(r.height), sig(cs)];
    for (const ps of ['::before', '::after']) { const pcs = getComputedStyle(el, ps); if (pcs.content && pcs.content !== 'none' && pcs.content !== 'normal') row.push(ps + sig(pcs)); }
    out.push(row.join('~')); }
  return out; }"""
result = {}
def snap(pg, key): result[key] = pg.evaluate(JS, PROPS)
try:
    seed_rich.events(base)
    with sync_playwright() as p:
        b = launch_chromium(p)
        def ctx(w, h, theme, scale):
            c = b.new_context(viewport={'width': w, 'height': h}, color_scheme='dark' if theme == 'dark' else 'light', reduced_motion='reduce')
            c.add_init_script(f"localStorage.setItem('yihe-text-scale','{scale}'); localStorage.setItem('yihe-theme','{theme}')"); return c
        for theme in ('light', 'dark'):
            for (w, h) in ((390, 844), (1280, 860)):
                c = ctx(w, h, theme, 1); pg = c.new_page()
                for page in ('home', 'about', 'services', 'contact', 'guide', 'privacy'):
                    pg.goto(base + f'/{page}.html'); pg.wait_for_timeout(450); snap(pg, f'public/{page}/{w}/{theme}')
                pg.goto(base + '/login.html'); pg.wait_for_selector('#loginSubmit'); pg.wait_for_timeout(300); snap(pg, f'login/{w}/{theme}')
                pg.fill('#loginName', 'res1'); pg.fill('#loginPassword', 'wrong'); pg.click('#loginSubmit'); pg.wait_for_timeout(700); snap(pg, f'login-error/{w}/{theme}')
                pg.fill('#loginPassword', 'correct-horse-3'); pg.click('#loginSubmit'); pg.wait_for_url('**/index.html'); pg.wait_for_selector('#residentVitals .vital-card', state='attached'); pg.wait_for_timeout(900)
                snap(pg, f'resident/home/{w}/{theme}')
                pg.evaluate("() => document.querySelector('.resident-metrics').open = true"); pg.wait_for_timeout(200); snap(pg, f'resident/home-open/{w}/{theme}')
                for tab in ('health', 'me'):
                    pg.click(f'[data-tab={tab}]'); pg.wait_for_timeout(250); snap(pg, f'resident/{tab}/{w}/{theme}')
                c.close()
                c = ctx(w, h, theme, 1); pg = c.new_page()
                pg.goto(base + '/管理端.html'); pg.wait_for_selector('#adminLoginSubmit'); pg.wait_for_timeout(300); snap(pg, f'staff-login/{w}/{theme}')
                pg.fill('#adminLoginName', 'nurse1'); pg.fill('#adminLoginPassword', 'correct-horse-1'); pg.click('#adminLoginSubmit'); pg.locator('#staffApp').wait_for(state='visible'); pg.wait_for_timeout(900)
                snap(pg, f'staff/modal/{w}/{theme}')
                pg.evaluate("() => document.getElementById('dangerModal').open && document.getElementById('dismissNewAlerts').click()"); pg.wait_for_timeout(250)
                def go(view):
                    # 窄屏下导航在抽屉里：用脚本触发，不依赖可见性
                    pg.evaluate("(v) => document.querySelector('[data-view=' + v + ']').click()", view); pg.wait_for_timeout(350)
                for v in ('summary', 'alerts', 'residents', 'map', 'more'):
                    go(v); snap(pg, f'staff/{v}/{w}/{theme}')
                go('alerts')
                pg.evaluate("() => [...document.querySelectorAll('[data-event]')].find(b => b.offsetParent)?.click()"); pg.locator('#eventDialog[open]').wait_for(timeout=4000); pg.wait_for_timeout(300); snap(pg, f'staff/event-dialog/{w}/{theme}')
                pg.keyboard.press('Escape'); pg.wait_for_timeout(200)
                go('residents')
                pg.evaluate("() => [...document.querySelectorAll('[data-resident]')].find(b => b.offsetParent)?.click()"); pg.locator('#residentDialog[open]').wait_for(timeout=4000); pg.wait_for_timeout(300); snap(pg, f'staff/resident-dialog/{w}/{theme}')
                c.close()
        for scale in (0.9, 1.15, 1.3):
            for (w, h) in ((390, 844), (1280, 860)):
                c = ctx(w, h, 'light', scale); pg = c.new_page()
                pg.goto(base + '/login.html'); pg.fill('#loginName', 'res1'); pg.fill('#loginPassword', 'correct-horse-3'); pg.click('#loginSubmit'); pg.wait_for_url('**/index.html'); pg.wait_for_selector('#residentVitals .vital-card', state='attached'); pg.wait_for_timeout(800)
                snap(pg, f'resident/home/{w}/scale{scale}')
                pg.goto(base + '/home.html'); pg.wait_for_timeout(300); snap(pg, f'public/home/{w}/scale{scale}'); c.close()
        b.close()
finally:
    os.killpg(os.getpgid(proc.pid), signal.SIGTERM)
print('states:', len(result), '| elements:', sum(len(v) for v in result.values()))
if mode == '--record':
    json.dump(result, open(target, 'w', encoding='utf-8'), ensure_ascii=False)
    print('recorded ->', target); sys.exit(0)
reference = json.load(open(target, encoding='utf-8'))
changed = sorted(k for k in set(reference) | set(result) if reference.get(k) != result.get(k))
for k in changed[:12]:
    rows = [(x, y) for x, y in zip(reference.get(k, []), result.get(k, [])) if x != y]
    print('DIFF', k, '| elements differing:', len(rows), '| count', len(reference.get(k, [])), '->', len(result.get(k, [])))
    if rows: print('   e.g.', rows[0][0][:110], '=>', rows[0][1][:110])
print('RESULT:', 'IDENTICAL' if not changed else f'{len(changed)} STATES DIFFER')
sys.exit(1 if changed else 0)
