"""Current community workflows; retired feature suites are not part of this test set."""
import functools
import http.server
import json
import os
import pathlib
import threading
import unittest
from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parents[1]
PUBLIC_PAGES = ['home.html','about.html','services.html','guide.html','contact.html','privacy.html']
STATIC_PAGES = PUBLIC_PAGES+['design-preview.html','login.html']
PAGE_READY = {'design-preview.html':'#previewResidents tr','login.html':'#loginSubmit','管理端.html':'#adminLoginSubmit'}
CONTRAST_JS = """window.__contrast = () => {
  const parse = value => (value.match(/[\\d.]+/g) || [0, 0, 0]).map(Number);
  const lum = value => {
    const c = parse(value).slice(0, 3).map(n => n / 255).map(n => (n <= 0.04045 ? n / 12.92 : Math.pow((n + 0.055) / 1.055, 2.4)));
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  };
  const bg = node => {
    for (let cur = node; cur; cur = cur.parentElement) {
      const p = parse(getComputedStyle(cur).backgroundColor);
      if (p.length === 3 || (p.length === 4 && p[3] > 0)) return getComputedStyle(cur).backgroundColor;
    }
    return 'rgb(255, 255, 255)';
  };
  return selector => {
    const n = document.querySelector(selector);
    if (!n) return null;
    const a = lum(getComputedStyle(n).color);
    const b = lum(bg(n));
    const high = Math.max(a, b) + 0.05;
    const low = Math.min(a, b) + 0.05;
    return Number((high / low).toFixed(2));
  };
};"""

class QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

class BrowserCase(unittest.TestCase):
    """浏览器夹具：静态 HTTP 服务 + Chromium + 每例独立上下文。子类的 setUp 可覆盖视口。"""
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(QuietHandler, directory=str(ROOT)))
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()
        cls.url = f'http://127.0.0.1:{cls.server.server_port}/'
        if (ROOT / '.pw-browsers').is_dir():
            os.environ.setdefault('PLAYWRIGHT_BROWSERS_PATH', str(ROOT / '.pw-browsers'))
        cls.pw = sync_playwright().start()
        engine = os.environ.get('YIHE_BROWSER', 'chromium')
        if engine not in ('chromium', 'firefox', 'webkit'):
            raise ValueError('YIHE_BROWSER must be chromium, firefox or webkit')
        browser_type = getattr(cls.pw, engine)
        options = {'headless': True}
        if os.environ.get('YIHE_BROWSER_PATH'):
            options['executable_path'] = os.environ['YIHE_BROWSER_PATH']
        cls.browser = browser_type.launch(**options)
        print(f'[browser] {engine} {cls.browser.version}: {options.get("executable_path", browser_type.executable_path)}')

    @classmethod
    def tearDownClass(cls):
        cls.browser.close(); cls.pw.stop(); cls.server.shutdown(); cls.server.server_close()

    def setUp(self):
        self.context = self.browser.new_context(viewport={'width':1440,'height':1050}, timezone_id='Asia/Shanghai')
        self.page = self.context.new_page(); self.page.set_default_timeout(7000)
        self.errors = []
        self.page.on('pageerror', lambda error:self.errors.append(str(error)))

    def tearDown(self):
        self.context.close()
        self.assertEqual(self.errors, [])

class CommunityTests(BrowserCase):
    def admin(self, page=None):
        p = page or self.page
        p.goto(self.url+'管理端.html')
        p.fill('#adminLoginName','admin');p.fill('#adminLoginPassword','admin123');p.click('#adminLoginSubmit')
        p.wait_for_function('() => document.querySelector("#workCounts button")')
        if p.locator('#dangerModal').evaluate('(x)=>x.open'):p.click('#dismissNewAlerts')
        p.evaluate('CareAPI.setSimulator(false)')

    def resident(self,page=None):
        p=page or self.page
        p.goto(self.url+'login.html');p.fill('#loginName','王国栋');p.fill('#loginPassword','123456');p.click('#loginSubmit')
        p.wait_for_url('**/index.html*');p.wait_for_function('() => !!window.App?.currentPatient')

    def nav(self,name):
        if self.page.viewport_size['width']<=1000:self.page.click('#hamburgerBtn')
        self.page.click('[data-view="'+name+'"]')

    def flow(self,kind):
        self.admin();self.nav('more');self.page.select_option('#simulationResident','1')
        self.page.click('[data-sim="'+kind+'"]')
        self.page.wait_for_function('(kind)=>JSON.parse(localStorage.getItem(CareAPI.KEY)).events.some(e=>e.residentId===1&&e.type===kind)',arg=kind)
        self.page.wait_for_function('()=>document.querySelector("#dangerModal").open')
        self.page.click('#viewNewAlerts')
        event=self.page.evaluate('(kind)=>CareAPI.snapshot().then(s=>s.events.find(e=>e.residentId===1&&e.type===kind))',kind)
        self.page.locator('#allEvents [data-event="'+event['id']+'"]').click()
        self.page.click('[data-event-action="claim"]');self.page.click('[data-event-action="processing"]')
        self.page.fill('#eventNote','已联系并核实，处置完成，已告知家属')
        self.page.click('[data-event-action="resolved"]')
        self.page.wait_for_function('(id)=>JSON.parse(localStorage.getItem(CareAPI.KEY)).events.find(e=>e.id===id).state==="resolved"',arg=event['id'])
        try:
            self.page.locator('.event-timeline li').nth(3).wait_for()
        except Exception:
            print(self.page.locator('#eventDialog').evaluate('(x)=>({open:x.open,text:x.innerText,html:x.innerHTML})'))
            raise
        self.assertGreaterEqual(self.page.locator('.event-timeline li').count(),4)
        self.page.reload();self.page.wait_for_function('()=>!!document.querySelector("#workCounts button")')
        saved=self.page.evaluate('(id)=>CareAPI.snapshot().then(s=>s.events.find(e=>e.id===id))',event['id'])
        self.assertEqual(saved['state'],'resolved');self.assertEqual(saved['owner'],'staff-1');self.assertTrue(saved['closedAt'])

    def test_health_discover_notify_claim_process_complete_trace(self):self.flow('health')
    def test_missed_dose_discover_notify_claim_process_complete_trace(self):self.flow('medication')
    def test_fence_discover_notify_claim_process_complete_trace(self):self.flow('fence')

    def test_resident_three_tabs_and_medication_sync(self):
        self.admin();self.page.evaluate('CareAPI.simulate(1,"medication")')
        resident=self.context.new_page();self.resident(resident)
        self.assertEqual(resident.locator('#bottomNav button').count(),3)
        resident.locator('#todayDoses button').click()
        resident.wait_for_function('()=>!!JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].doses.at(-1).confirmedAt')
        self.assertTrue(self.page.evaluate('CareAPI.snapshot().then(s=>s.events.some(e=>e.type==="medication"&&e.sourceRecoveredAt&&e.state==="new"))'))
        resident.click('[data-tab="health"]');self.assertIn('已服药',resident.locator('#doseHistory').inner_text())
        resident.reload();resident.wait_for_function('()=>!!window.App?.currentPatient')
        self.assertIn('已服药',resident.locator('#doseHistory').inner_text())

    def test_failed_claim_keeps_state_and_result_input(self):
        self.admin();self.page.evaluate('CareAPI.simulate(1,"health")');self.page.reload();self.page.wait_for_function('()=>!!document.querySelector("#workCounts button")')
        if self.page.locator('#dangerModal').evaluate('(x)=>x.open'):self.page.click('#dismissNewAlerts')
        event=self.page.evaluate('CareAPI.snapshot().then(s=>s.events.find(e=>e.residentId===1&&e.type==="health"))')
        self.page.locator('#todoEvents [data-event="'+event['id']+'"]').click();self.page.fill('#eventNote','保留输入')
        self.page.evaluate('''() => { const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k===CareAPI.KEY)throw Error('quota');return original.call(this,k,v);}; }''')
        self.page.click('[data-event-action="claim"]');self.page.wait_for_selector('#eventError:not([hidden])')
        self.assertEqual(self.page.input_value('#eventNote'),'保留输入')
        self.assertEqual(self.page.evaluate('(id)=>CareAPI.snapshot().then(s=>s.events.find(e=>e.id===id).state)',event['id']),'new')

    def test_cross_tab_competing_claims(self):
        self.admin();other=self.context.new_page();self.admin(other)
        self.page.evaluate('CareAPI.simulate(1,"fence")')
        event=self.page.evaluate('CareAPI.snapshot().then(s=>s.events.find(e=>e.residentId===1&&e.type==="fence"))')
        self.page.evaluate('''() => {
          const request=navigator.locks.request.bind(navigator.locks);
          const channel=new BroadcastChannel('claim-race');
          navigator.locks.request=(name,options,callback)=>request(name,options,async lock=>{
            if(window.holdClaimLock){window.holdClaimLock=false;channel.postMessage('locked');await new Promise(resolve=>{
              channel.onmessage=({data})=>{if(data==='release')resolve();};
            });}
            return callback(lock);
          });
          window.holdClaimLock=true;
        }''')
        other.evaluate('''(e) => {
          const channel=new BroadcastChannel('claim-race');
          channel.onmessage=({data})=>{
            if(data!=='locked')return;
            window.claimResult=CareAPI.transition(e.id,'claim','staff-2','',e.revision).then(()=>"ok",()=>"rejected");
            channel.postMessage('release');
          };
        }''',event)
        self.page.evaluate('(e)=>{window.claimResult=CareAPI.transition(e.id,"claim","staff-1","",e.revision).then(()=>"ok",()=>"rejected")}',event)
        self.assertEqual(sorted([self.page.evaluate('claimResult'),other.evaluate('claimResult')]),['ok','rejected'])
        owner=other.evaluate('(id)=>CareAPI.snapshot().then(s=>s.events.find(e=>e.id===id).owner)',event['id'])
        self.assertIn(owner,['staff-1','staff-2'])

    def test_context_responsibility_contacts_and_dose_plan(self):
        self.admin();self.nav('residents');self.page.locator('[data-resident="1"]').first.click()
        self.assertIn('采样',self.page.locator('#residentBody').inner_text())
        maintenance=self.page.locator('details.resident-maintenance')
        self.assertFalse(maintenance.evaluate('(x)=>x.open'))
        maintenance.locator('summary').click()
        self.assertTrue(maintenance.evaluate('(x)=>x.open'))
        self.page.select_option('#responsibilityForm select','staff-2');self.page.locator('#responsibilityForm button[type="submit"]').click()
        self.page.wait_for_function('()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].responsible==="staff-2"')
        self.assertTrue(maintenance.evaluate('(x)=>x.open'))
        self.page.fill('#staffContactForm [name="name"]','测试家属');self.page.fill('#staffContactForm [name="phone"]','13800000000');self.page.locator('#staffContactForm button[type="submit"]').click()
        self.page.wait_for_function('()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].contacts.length>0')
        self.page.locator('#residentBody a[href="tel:13800000000"]').wait_for()
        self.assertEqual(self.page.locator('#residentBody a[href="tel:13800000000"]').count(),1)
        self.page.fill('#doseForm [name="name"]','已核对的演示任务')
        due=self.page.evaluate('()=>{const d=new Date(Date.now()+3600000);return new Date(d.getTime()-d.getTimezoneOffset()*60000).toISOString().slice(0,16)}')
        self.page.fill('#doseForm [name="dueAt"]',due);self.page.check('#doseForm [name="verified"]');self.page.locator('#doseForm button[type="submit"]').click()
        self.page.wait_for_function('()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].doses.length===1')

    def test_mask_export_and_shared_theme(self):
        self.admin();self.nav('more');self.page.check('#settingMask');self.page.check('#settingDarkMode')
        self.assertEqual(self.page.get_attribute('html','data-theme'),'dark')
        self.page.evaluate('CareAPI.simulate(1,"fence")');self.page.reload();self.page.wait_for_function('()=>!!document.querySelector("#workCounts button")')
        if self.page.locator('#dangerModal').evaluate('(x)=>x.open'):self.page.click('#dismissNewAlerts')
        self.nav('more')
        with self.page.expect_download() as download:self.page.click('#exportEvents')
        data=json.loads(pathlib.Path(download.value.path()).read_text(encoding='utf-8'))
        self.assertNotIn('王国栋',[e['residentName'] for e in data['events']])
        other=self.context.new_page();other.goto(self.url+'home.html');self.assertEqual(other.get_attribute('html','data-theme'),'dark')
        self.page.uncheck('#settingDarkMode');other.wait_for_function('()=>document.documentElement.dataset.theme==="light"')

    def test_responsive_screenshots_and_no_overflow(self):
        self.resident()
        artifact=os.environ.get('YIHE_ARTIFACT_DIR')
        for mode in ['resident','staff']:
            if mode=='staff':self.admin()
            for width in [320,390,768,1440]:
                self.page.set_viewport_size({'width':width,'height':950})
                # 由宽变窄时侧栏会从文档流切到 fixed 并带 transform 过渡（约 180ms），
                # 立即量尺寸/截图会截到滑动中途的半截侧栏；等所有动画结束再取证。
                self.page.wait_for_function('()=>document.getAnimations().length===0')
                offenders=self.page.evaluate('''() => [...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>innerWidth+1&&e.getClientRects().length).slice(0,8).map(e=>[e.tagName,e.className,e.getBoundingClientRect().right])''')
                self.assertLessEqual(self.page.evaluate('document.documentElement.scrollWidth'),width,str(offenders))
                if artifact:
                    # 只截视口：full_page 会让 sticky 顶栏与离屏侧栏错位（窄屏下会把桌面侧栏渲进来），
                    # 产出的图与用户实际看到的画面不符，不能作为验收证据。
                    pathlib.Path(artifact).mkdir(parents=True,exist_ok=True)
                    self.page.screenshot(path=str(pathlib.Path(artifact)/f'{mode}-{width}.png'))

    def test_todo_count_buttons_follow_selected_filter(self):
        self.admin()
        for key in ['all','mine','unassigned','overdue']:
            button=self.page.locator('.work-count[data-todo="'+key+'"]')
            button.click()
            self.assertEqual(self.page.locator('.work-count[aria-pressed="true"]').count(),1)
            self.assertEqual(self.page.locator('.work-count[aria-pressed="true"]').get_attribute('data-todo'),key)
            self.assertEqual(self.page.input_value('#todoFilter'),key)

    def test_event_search_matches_resident_and_original_detail_and_reset(self):
        self.admin()
        events=self.page.evaluate("async()=>{const before=new Set((await CareAPI.snapshot()).events.map(e=>e.id));await Promise.all([CareAPI.simulate(1,'health'),CareAPI.simulate(2,'fence')]);const s=await CareAPI.snapshot();return s.events.filter(e=>!before.has(e.id)).map(e=>({id:e.id,name:s.residents[e.residentId].name,detail:e.detail}))}")
        self.assertEqual(len(events),2)
        if self.page.locator('#dangerModal').evaluate('(x)=>x.open'): self.page.click('#dismissNewAlerts')
        self.nav('alerts')
        for event in events: self.page.locator('#allEvents [data-event="'+event['id']+'"]').wait_for()
        self.page.fill('#eventSearch',events[0]['name'])
        self.page.wait_for_function("(id)=>{const rows=document.querySelectorAll('#allEvents [data-event]');return rows.length===1&&rows[0].dataset.event===id}",arg=events[0]['id'])
        self.page.fill('#eventSearch',events[1]['detail'])
        self.page.wait_for_function("(id)=>{const rows=document.querySelectorAll('#allEvents [data-event]');return rows.length===1&&rows[0].dataset.event===id}",arg=events[1]['id'])
        self.page.select_option('#typeFilter','health')
        self.page.wait_for_function("()=>document.querySelectorAll('#allEvents [data-event]').length===0")
        self.page.click('#resetEventFilters')
        self.assertEqual(self.page.input_value('#eventSearch'),'')
        self.assertEqual(self.page.input_value('#eventFilter'),'open')
        self.assertEqual(self.page.input_value('#typeFilter'),'')
        opened=self.page.evaluate("()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).events.filter(CareAPI.isOpen).length")
        self.assertGreaterEqual(opened,len(events))
        self.page.wait_for_function("(n)=>document.querySelectorAll('#allEvents [data-event]').length===n",arg=opened)
        for event in events: self.assertEqual(self.page.locator('#allEvents [data-event="'+event['id']+'"]').count(),1)

    def test_event_cards_and_dialog_show_actionable_stages(self):
        self.admin();self.page.evaluate("CareAPI.simulate(1,'health')")
        self.page.wait_for_function("()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).events.some(e=>e.residentId===1&&e.type==='health')")
        if self.page.locator('#dangerModal').evaluate('(x)=>x.open'): self.page.click('#dismissNewAlerts')
        event=self.page.evaluate("CareAPI.snapshot().then(s=>s.events.find(e=>e.residentId===1&&e.type==='health'))")
        self.nav('alerts')
        action=self.page.locator('#allEvents [data-event="'+event['id']+'"]').first
        action.wait_for()
        card=action.locator('xpath=ancestor::article[1]')
        self.assertTrue(card.locator('.severity-tag').is_visible())
        self.assertTrue(card.locator('.event-deadline').is_visible())
        action.click();self.page.locator('#eventDialog').wait_for(state='visible')
        self.assertTrue(self.page.locator('.disposition-steps').is_visible())
        self.assertEqual(self.page.locator('.event-actions [data-event-action].btn-primary').count(),1)
        self.page.click('[data-event-action="claim"]')
        self.page.wait_for_function("()=>{const b=document.querySelector('.event-actions [data-event-action].btn-primary');return !!b&&b.dataset.eventAction==='processing'}")
        self.assertEqual(self.page.locator('.event-actions [data-event-action].btn-primary').count(),1)
        self.page.click('[data-event-action="processing"]')
        self.page.wait_for_function("()=>{const b=document.querySelector('.event-actions [data-event-action].btn-primary');return !!b&&b.dataset.eventAction==='resolved'}")
        self.assertEqual(self.page.locator('.event-actions [data-event-action].btn-primary').count(),1)

    def test_resident_home_contact_shortcut_touch_targets_and_primary_contrast(self):
        self.page.set_viewport_size({'width':390,'height':844})
        # 切换主题会触发 .btn 的 background-color 过渡；在过渡途中读色取到的是插值，会误判对比度。
        # 项目在 prefers-reduced-motion 下关闭这些过渡，读到的才是最终色值。
        self.page.emulate_media(reduced_motion='reduce')
        self.resident()
        shortcut=self.page.locator('.resident-contact-jump[href="#emergencyContacts"]')
        self.assertTrue(shortcut.is_visible())
        bounds=shortcut.bounding_box()
        self.assertGreaterEqual(bounds['y'],0);self.assertLessEqual(bounds['y']+bounds['height'],844)
        shortcut.click()
        self.page.wait_for_function("()=>{const y=document.querySelector('#emergencyContacts').getBoundingClientRect().top;return y>=0&&y<innerHeight}")
        target=self.page.locator('#emergencyContacts').bounding_box()
        self.assertGreaterEqual(target['y'],0);self.assertLessEqual(target['y'],844)
        self.assertGreaterEqual(self.page.locator('.resident-care button:visible').evaluate_all("xs=>Math.min(...xs.map(x=>x.getBoundingClientRect().height))"),52)
        self.page.click('[data-tab="me"]')
        for theme in ['light','dark']:
            self.page.evaluate('(theme)=>YiheTheme.set(theme)',theme);self.page.wait_for_function('()=>document.getAnimations().length===0')
            ratio=self.page.locator('.resident-care .btn-primary').first.evaluate('''x=>{const c=getComputedStyle(x),rgb=v=>v.match(/[\\d.]+/g).slice(0,3).map(Number),lum=v=>{const a=rgb(v).map(n=>n/255).map(n=>n<=.04045?n/12.92:((n+.055)/1.055)**2.4);return .2126*a[0]+.7152*a[1]+.0722*a[2]},a=lum(c.color),b=lum(c.backgroundColor);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05)}''')
            self.assertGreaterEqual(ratio,4.5,theme)
        self.admin()
        self.assertGreaterEqual(self.page.locator('.staff-care button:visible').evaluate_all("xs=>Math.min(...xs.map(x=>x.getBoundingClientRect().height))"),44)
        for theme in ['light','dark']:
            self.page.evaluate('(theme)=>YiheTheme.set(theme)',theme);self.page.wait_for_function('()=>document.getAnimations().length===0')
            ratio=self.page.locator('.staff-care .btn-primary').first.evaluate('''x=>{const c=getComputedStyle(x),rgb=v=>v.match(/[\\d.]+/g).slice(0,3).map(Number),lum=v=>{const a=rgb(v).map(n=>n/255).map(n=>n<=.04045?n/12.92:((n+.055)/1.055)**2.4);return .2126*a[0]+.7152*a[1]+.0722*a[2]},a=lum(c.color),b=lum(c.backgroundColor);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05)}''')
            self.assertGreaterEqual(ratio,4.5,theme)

    def test_resident_dose_disabled_until_window_and_offline_sample_label(self):
        self.admin()
        self.page.evaluate("CareAPI.addDose(1,'UX dose window',new Date(Date.now()+60*60000).toISOString(),'staff-1')")
        self.page.wait_for_function("()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].doses.some(d=>d.name==='UX dose window')")
        self.resident()
        dose=self.page.locator('#todayDoses .dose-row').filter(has_text='UX dose window')
        button=dose.locator('button[data-dose]')
        self.assertTrue(button.is_disabled())
        self.assertEqual(button.inner_text(),''.join(map(chr,[23578,26410,21040,30830,35748,26102,38388])))
        dose_id=button.get_attribute('data-dose')
        # 未到确认窗口：按钮是 disabled，Playwright 默认拒绝点击，只有 force 能验证点了也不生效
        button.click(force=True)
        self.assertIsNone(self.page.evaluate('(id)=>CareAPI.snapshot().then(s=>s.residents[1].doses.find(d=>d.id===id).confirmedAt||null)',dose_id))
        self.page.evaluate('''(id)=>{const s=JSON.parse(localStorage.getItem(CareAPI.KEY));s.residents[1].doses.find(d=>d.id===id).dueAt=new Date(Date.now()+20*60000).toISOString();localStorage.setItem(CareAPI.KEY,JSON.stringify(s));window.dispatchEvent(new Event('care:change'));}''',dose_id)
        self.page.wait_for_function("id=>!!document.querySelector('#todayDoses [data-dose=\\\"'+id+'\\\"]:not(:disabled)')",arg=dose_id)
        self.page.locator('#todayDoses [data-dose="'+dose_id+'"]').click()
        self.page.wait_for_function('(id)=>CareAPI.snapshot().then(s=>!!s.residents[1].doses.find(d=>d.id===id).confirmedAt)',arg=dose_id)
        self.page.evaluate('CareAPI.simulate(1,"offline")')
        self.page.wait_for_function("()=>document.querySelector('.home-health').dataset.status!=='normal'")
        details=self.page.locator('details.resident-metrics');details.locator('summary').click()
        self.assertTrue(details.locator('.vital-card').first.is_visible())
        self.assertIn(''.join(map(chr,[31163,32447,21069,37319,26679])),details.inner_text())

    def test_zoom_200_percent_layout_has_no_horizontal_overflow(self):
        # 真实 200% 缩放 = CSS 视口减半 + deviceScaleFactor 2，媒体查询随之重排。
        # 旧写法 body.style.zoom=2 只放大渲染、不触发媒体查询，对固定最小列布局会误报溢出，故弃用。
        for mode in ['resident','staff']:
            zoomed=self.browser.new_context(viewport={'width':384,'height':950},device_scale_factor=2,timezone_id='Asia/Shanghai')
            page=zoomed.new_page();page.set_default_timeout(7000)
            try:
                self.resident(page) if mode=='resident' else self.admin(page)
                width=page.evaluate('document.documentElement.scrollWidth')
                self.assertLessEqual(width,384,{'mode':mode,'zoom':'200%','scrollWidth':width})
            finally:
                zoomed.close()

    def test_community_pages_have_no_chat_dependency_or_deepseek_policy(self):
        for route in ['home.html','\u7ba1\u7406\u7aef.html','index.html']:
            self.page.goto(self.url+route)
            self.assertEqual(self.page.locator('script[src*="chat" i],a[href*="chat" i]').count(),0,route)
            policy=self.page.locator('meta[http-equiv="Content-Security-Policy"]').get_attribute('content') or ''
            self.assertNotIn('deepseek',policy.lower(),route)

    def test_no_deprecated_navigation_and_public_links(self):
        self.admin();self.assertEqual(self.page.locator('.menu-item').count(),5)
        self.assertEqual(self.page.locator('[data-view="admission"],[data-view="sharing"],[data-view="audit"]').count(),0)
        for route in ['home.html','services.html','about.html','guide.html','contact.html','privacy.html']:
            self.page.goto(self.url+route)
            self.assertEqual(self.page.locator('a[href*="news"],a[href*="yangheng"]').count(),0)

    def test_pagination_sizes_reset_and_empty(self):
        self.admin()
        for count in [12,24,25,30]:
            self.page.evaluate('''count=>{const s=JSON.parse(localStorage.getItem(CareAPI.KEY)),p=s.residents[1];s.residents={};for(let id=1;id<=count;id++)s.residents[id]={...p,id,name:'居民'+String(id).padStart(2,'0')};s.revision++;localStorage.setItem(CareAPI.KEY,JSON.stringify(s));}''',count)
            self.page.reload();self.page.wait_for_selector('#workCounts button',state='attached')
            if self.page.locator('#dangerModal').evaluate('(x)=>x.open'):self.page.click('#dismissNewAlerts')
            self.nav('residents')
            self.assertEqual(self.page.locator('#residentList [data-resident]').count(),12)
            if count>12:
                while self.page.locator('[data-page="next"]').is_enabled():self.page.click('[data-page="next"]')
                self.assertEqual(self.page.locator('#residentList [data-resident]').count(),count%12 or 12)
                self.assertTrue(self.page.locator('[data-page="next"]').is_disabled())
            self.page.fill('#residentSearch','居民01');self.page.wait_for_function('()=>document.querySelectorAll("#residentList [data-resident]").length===1');self.assertEqual(self.page.locator('#residentList [data-resident]').count(),1)
            self.page.fill('#residentSearch','没有这个人');self.page.wait_for_function('()=>document.querySelectorAll("#residentList [data-resident]").length===0');self.assertEqual(self.page.locator('#residentPagination button').count(),0)
            self.page.fill('#residentSearch','')
            if count>12:self.assertTrue(self.page.locator('[data-page="prev"]').is_disabled())

    def test_audit_rotation_legacy_tampering_and_failed_save(self):
        self.admin()
        result=self.page.evaluate('''()=>{Audit.clear();for(let i=0;i<1000;i++)Audit.log('test',String(i));return {length:Audit.all().length,check:Audit.verify()}}''')
        self.assertEqual(result['length'],500);self.assertTrue(result['check']['ok']);self.assertTrue(result['check']['retainedOnly'])
        self.assertFalse(self.page.evaluate('''()=>{const s=JSON.parse(localStorage.getItem('hm-audit'));s.records[0].detail='changed';localStorage.setItem('hm-audit',JSON.stringify(s));return Audit.verify().ok}'''))
        self.page.evaluate('localStorage.setItem("hm-audit",JSON.stringify([{ts:1,action:"legacy",detail:"keep"}]))')
        self.page.evaluate('Audit.log("next","entry")');self.assertEqual(self.page.evaluate('Audit.all()[0].detail'),'keep')
        self.page.evaluate('''()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==='hm-audit')throw Error('quota');return original.call(this,k,v);};}''')
        self.assertFalse(self.page.evaluate('Audit.log("fail","not saved").ok'));self.assertEqual(self.page.evaluate('Audit.all().length'),2)

    def test_theme_failure_and_migration_precedence(self):
        self.page.goto(self.url+'login.html')
        self.page.evaluate('localStorage.setItem("hm-p-theme",JSON.stringify("dark"));localStorage.setItem("hm-settings",JSON.stringify({darkMode:false}))')
        self.page.reload();self.assertEqual(self.page.get_attribute('html','data-theme'),'dark')
        self.page.evaluate('localStorage.setItem("yihe-theme","light")');self.admin();self.nav('more')
        self.assertEqual(self.page.get_attribute('html','data-theme'),'light')
        color=self.page.evaluate('getComputedStyle(document.body).backgroundColor')
        self.page.evaluate('''()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==='yihe-theme')throw Error('quota');return original.call(this,k,v);};}''')
        self.page.click('#settingDarkMode')
        self.assertFalse(self.page.is_checked('#settingDarkMode'));self.assertEqual(self.page.get_attribute('html','data-theme'),'light')
        self.assertEqual(self.page.evaluate('getComputedStyle(document.body).backgroundColor'),color)

    def test_contact_failure_does_not_claim_success(self):
        self.page.goto(self.url+'contact.html')
        self.assertFalse(self.page.locator('.form-success').is_visible())
        self.page.locator('input[name="name"]').fill('演示访客')
        self.page.locator('input[name="phone"]').fill('13800000000')
        self.page.locator('textarea[name="message"]').fill('仅测试本机表单')
        self.page.evaluate("() => {Storage.prototype.setItem=function(){throw new DOMException('quota','QuotaExceededError')}}")
        self.page.locator('form button[type="submit"]').click()
        self.assertTrue(self.page.locator('#messageError').is_visible())
        self.assertIn('保存失败',self.page.locator('#messageError').inner_text())
        self.assertFalse(self.page.locator('.form-success').is_visible())

    def test_contact_success_only_after_local_save(self):
        self.page.goto(self.url+'contact.html')
        self.assertFalse(self.page.locator('.form-success').is_visible())
        self.page.locator('input[name="name"]').fill('演示访客')
        self.page.locator('input[name="phone"]').fill('13800000000')
        self.page.locator('textarea[name="message"]').fill('仅测试本机表单')
        self.page.locator('form button[type="submit"]').click()
        self.assertTrue(self.page.locator('.form-success').is_visible())
        self.assertEqual(self.page.evaluate('JSON.parse(localStorage.getItem("yihe-messages")).length'),1)

    def test_mobile_keyboard_navigation_and_dialog_focus(self):
        self.page.set_viewport_size({'width':390,'height':844});self.admin();self.page.click('#hamburgerBtn')
        self.page.locator('[data-view="residents"]').focus();self.page.keyboard.press('Enter')
        self.assertTrue(self.page.locator('#view-residents').evaluate('(x)=>x.classList.contains("active")'))
        self.page.locator('[data-resident="1"]').first.focus();self.page.keyboard.press('Enter')
        self.assertTrue(self.page.locator('#residentDialog').evaluate('(x)=>x.open'))
        self.page.keyboard.press('Escape');self.assertFalse(self.page.locator('#residentDialog').evaluate('(x)=>x.open'))

    def test_contact_and_profile_survive_reload_and_name_login(self):
        self.resident();self.page.click('[data-tab="me"]');self.page.fill('#profileName','测试居民');self.page.fill('#profileAge','73')
        self.page.locator('#residentProfileForm button[type="submit"]').click();self.page.wait_for_function('()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].name==="测试居民"')
        self.page.fill('#contactForm [name="name0"]','测试家属');self.page.fill('#contactForm [name="phone0"]','13800000000');self.page.locator('#contactForm button[type="submit"]').click()
        self.page.wait_for_function('()=>JSON.parse(localStorage.getItem(CareAPI.KEY)).residents[1].contacts.length===1')
        self.page.click('#residentLogout');self.page.wait_for_url('**/login.html');self.page.fill('#loginName','测试居民');self.page.fill('#loginPassword','123456');self.page.click('#loginSubmit')
        self.page.wait_for_url('**/index.html*');self.page.wait_for_selector('#homeContacts a[href="tel:13800000000"]')
        self.assertIn('测试居民',self.page.locator('#greetingName').inner_text())

class PublicPagesTests(BrowserCase):
    """公共页与组件校对页：迁移 测试/legacy 中尚未覆盖的对比度、触控尺寸、200% 缩放与预览视觉断言。"""
    def setUp(self):
        super().setUp()
        self.page.set_viewport_size({'width':320,'height':900})
        self.page.add_init_script(CONTRAST_JS)
        # 主题切换带 .18s 过渡，过渡途中读色得到的是插值；项目在"减少动态效果"下关闭过渡，读到的才是最终色值。
        self.page.emulate_media(reduced_motion='reduce')

    def public(self,name):
        self.page.goto(self.url+name);self.page.wait_for_selector('#navToggle')

    def contrast(self,selector):
        return self.page.evaluate('(s)=>window.__contrast()(s)',selector)

    OFFENDERS = "()=>[...document.querySelectorAll('body *')].filter(e=>e.getBoundingClientRect().right>innerWidth+1&&e.getClientRects().length).slice(0,6).map(e=>[e.tagName,e.className,Math.round(e.getBoundingClientRect().right)])"

    def settled(self,width,height=950):
        """改视口后先等动画结束：断点切换时侧栏/抽屉有 transform 过渡，量到的是中途状态。"""
        self.page.set_viewport_size({'width':width,'height':height})
        self.page.wait_for_function('()=>document.getAnimations().length===0')

    def test_public_pages_at_four_widths_and_header_touch_targets(self):
        for name in PUBLIC_PAGES:
            self.public(name)
            for width in [320,390,768,1440]:
                self.settled(width)
                self.assertLessEqual(self.page.evaluate('document.documentElement.scrollWidth'),width,(name,width,self.page.evaluate(self.OFFENDERS)))
            # 触控目标只在窄屏导航态存在：>920px 侧栏常驻、汉堡按钮隐藏。
            for width in [320,390,768]:
                self.settled(width)
                sizes=self.page.eval_on_selector_all('#navToggle,.font-ctrl button,#themeToggle',"nodes=>nodes.filter(n=>n.getClientRects().length).map(n=>[n.id||n.dataset.font,n.getBoundingClientRect().width,n.getBoundingClientRect().height])")
                self.assertEqual(len(sizes),4,(name,width))
                self.assertTrue(all(w>=44 and h>=44 for _,w,h in sizes),(name,width,sizes))
        self.settled(320,900)
        self.public('home.html')
        self.page.click('#navToggle')
        self.assertEqual(self.page.locator('#navToggle').get_attribute('aria-expanded'),'true')
        self.assertTrue(self.page.locator('.nav-links a.nav-cta').is_visible())

    def test_public_primary_actions_meet_contrast_in_both_themes(self):
        # 透明背景要向上回溯到第一个不透明祖先，否则会把 rgba(0,0,0,0) 当黑色算出虚高对比度。
        targets={'home.html':['.nav-cta','.btn--primary','.brand-seal'],
                 'login.html':['.login-seal','.login-btn-cn'],
                 '管理端.html':['.login-btn'],
                 'about.html':['.nav-cta','.brand-seal'],
                 'services.html':['.nav-cta','.brand-seal'],
                 'guide.html':['.nav-cta','.brand-seal'],
                 'contact.html':['.nav-cta','.brand-seal'],
                 'privacy.html':['.nav-cta','.brand-seal']}
        for name,selectors in targets.items():
            self.page.goto(self.url+name);self.page.wait_for_selector(PAGE_READY.get(name,'#navToggle'))
            for theme in ['light','dark']:
                self.page.evaluate('(theme)=>YiheTheme.set(theme)',theme)
                self.page.wait_for_function('()=>document.getAnimations().length===0')
                self.assertEqual(self.page.get_attribute('html','data-theme'),theme,name)
                for selector in selectors:
                    ratio=self.contrast(selector)
                    self.assertIsNotNone(ratio,(name,selector))
                    self.assertGreaterEqual(ratio,4.5,(name,theme,selector,ratio))

    def test_static_pages_at_200_percent_zoom_have_no_horizontal_overflow(self):
        # 200% 缩放 = CSS 视口减半 + deviceScaleFactor 2；400% 等价的 320px 由 test_responsive_screenshots_and_no_overflow 覆盖。
        for name in STATIC_PAGES:
            zoomed=self.browser.new_context(viewport={'width':384,'height':950},device_scale_factor=2,timezone_id='Asia/Shanghai')
            page=zoomed.new_page();page.set_default_timeout(7000)
            try:
                page.goto(self.url+name);page.wait_for_selector(PAGE_READY.get(name,'#navToggle'))
                width=page.evaluate('document.documentElement.scrollWidth')
                self.assertLessEqual(width,384,{'page':name,'zoom':'200%','scrollWidth':width})
            finally:
                zoomed.close()

    def test_design_preview_seed_states_and_tokens(self):
        self.page.set_viewport_size({'width':390,'height':844})
        self.page.goto(self.url+'design-preview.html')
        self.page.wait_for_function('()=>document.querySelectorAll("#previewResidents tr").length===4')
        self.assertIn('王国栋',self.page.locator('#previewResidents').inner_text())
        self.assertIn('演示基线',self.page.locator('#previewResidents tr').first.inner_text())
        # 校对页只演示控件状态，不得写出本机监护记录。
        self.assertIsNone(self.page.evaluate("()=>localStorage.getItem('yihe-community-v1')"))
        self.page.click('#previewTheme')
        self.assertEqual(self.page.get_attribute('html','data-theme'),'dark')
        self.assertEqual(self.page.locator('#previewTheme').get_attribute('aria-pressed'),'true')
        self.assertEqual(self.page.locator('#previewTheme').inner_text(),'切换浅色')
        self.page.click('#previewDialog')
        self.page.locator('#previewOverlay').wait_for(state='visible')
        self.assertEqual(self.page.evaluate('document.activeElement.id'),'previewCancel')
        self.page.keyboard.press('Shift+Tab')
        self.assertEqual(self.page.evaluate('document.activeElement.id'),'previewConfirm')
        self.page.keyboard.press('Escape')
        self.assertFalse(self.page.locator('#previewOverlay').is_visible())
        self.assertEqual(self.page.evaluate('document.activeElement.id'),'previewDialog')
        self.page.click('#previewToast')
        self.assertIn('演示数据未改变',self.page.locator('#previewToastMessage').inner_text())
        self.page.click('#previewDanger')
        self.assertIn('不会生成预警',self.page.locator('#previewToastMessage').inner_text())
        self.assertTrue(self.page.locator('.dp-button[disabled]').is_disabled())
        self.assertGreaterEqual(self.page.evaluate("()=>parseFloat(getComputedStyle(document.querySelector('.dp-button')).borderRadius)"),6)
        for theme in ['light','dark']:
            self.page.evaluate('(theme)=>YiheTheme.set(theme)',theme)
            self.page.wait_for_function('()=>document.getAnimations().length===0')
            for selector in ['.dp-primary','.dp-help','.dp-badge.normal','.dp-badge.warning','.dp-badge.danger','.dp-badge.pending']:
                ratio=self.contrast(selector)
                self.assertIsNotNone(ratio,(theme,selector))
                self.assertGreaterEqual(ratio,4.5,(theme,selector,ratio))
            self.assertGreaterEqual(self.page.locator('.dp-button:visible').first.bounding_box()['height'],44,theme)

if __name__=='__main__':unittest.main()
