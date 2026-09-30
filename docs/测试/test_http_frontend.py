"""Exercise the built product in a browser with an explicit simulated API contract."""
import copy
import json
from pathlib import Path
from urllib.parse import urlsplit
import test_community as community


class HttpFrontendTests(community.BrowserCase):
    def setUp(self):
        super().setUp()
        self.assertTrue((community.ROOT/'dist/index.html').exists(), 'Run npm run build first')
        self.base=self.url+'dist/'
        self.snapshot=json.loads((Path(__file__).parent/'fixtures/http-snapshot.json').read_text(encoding='utf-8'))
        self.user=None
        self.requests=[]
        self.fail_snapshot=None
        self.fail_write=None
        self.context.route('**/api/v1/**',self.api)

    def api(self,route):
        request=route.request
        path=urlsplit(request.url).path
        method=request.method
        body=request.post_data_json if request.post_data else None
        self.requests.append((method,path,body,request.headers))
        if method!='GET':
            self.assertEqual(request.headers.get('x-csrf-token'),'csrf-test')
        if path=='/api/v1/session':
            if method=='POST':
                self.user={'role':body['role'],'name':'服务账户',**({'residentId':101} if body['role']=='resident' else {'staffId':'care-nurse'})}
            if method=='DELETE': self.user=None
            route.fulfill(json={'user':self.user,'csrfToken':'csrf-test'});return
        if path=='/api/v1/care/snapshot':
            if self.fail_snapshot=='network':route.abort();return
            if self.fail_snapshot=='invalid':route.fulfill(json={'residents':{}});return
            if self.fail_snapshot==401:
                self.user=None;route.fulfill(status=401,json={});return
            route.fulfill(json=copy.deepcopy(self.snapshot));return
        if self.fail_write:
            route.fulfill(status=self.fail_write,json={'message':'private internal detail'});return
        if path=='/api/v1/events/presented':
            for shown in body['events']:
                for event in self.snapshot['events']:
                    if event['id']==shown['id']:
                        self.assertLessEqual(shown['cycle'],event['notification']['cycle'])
                        event['notification']['presentedCycle']=shown['cycle']
        elif path=='/api/v1/residents/101':
            resident=self.snapshot['residents']['101']
            if body.get('expectedRevision')!=resident['revision']:
                route.fulfill(status=409,json={});return
            for patch in body.get('contactUpdates',[]):resident['contacts'][patch['index']]=patch['value']
            resident.update({key:value for key,value in body.items() if key not in ['expectedRevision','contactUpdates']})
            resident['revision']+=1;self.snapshot['revision']+=1
        elif path.endswith('/transitions'):
            event=self.snapshot['events'][0]
            if body['revision']!=event['revision']:route.fulfill(status=409,json={});return
            event['state']={'claim':'claimed'}.get(body['action'],body['action'])
            if body['action']=='claim':event['notification']['acknowledgedCycle']=event['notification']['cycle']
            event['owner']='care-nurse';event['revision']+=1;self.snapshot['revision']+=1
        route.fulfill(status=204)

    def resident(self):
        self.page.goto(self.base+'login.html')
        self.page.fill('#loginName','resident-account');self.page.fill('#loginPassword','test-password')
        self.page.evaluate('document.querySelector("#loginSubmit").click();document.querySelector("#loginSubmit").click()')
        self.page.wait_for_url('**/dist/index.html*')
        self.page.wait_for_function('() => window.App?.USER_ID===101')

    def staff(self):
        self.page.goto(self.base+'管理端.html')
        self.page.fill('#adminLoginName','server-staff');self.page.fill('#adminLoginPassword','test-password');self.page.click('#adminLoginSubmit')
        self.page.wait_for_function('() => !!document.querySelector("#workCounts button")')
        if self.page.locator('#dangerModal').evaluate('(node)=>node.open'):self.page.click('#dismissNewAlerts')

    def test_resident_server_login_validation_save_and_logout(self):
        self.resident()
        posts=[r for r in self.requests if r[0]=='POST' and r[1].endswith('/session')]
        self.assertEqual(len(posts),1)
        self.assertIsNone(self.page.evaluate('localStorage.getItem("hm-login")'))
        self.assertIsNone(self.page.evaluate('localStorage.getItem("yihe-community-v1")'))
        self.page.click('[data-tab="me"]');self.page.fill('#profileName','新姓名')
        self.fail_write=422;self.page.click('#residentProfileForm button')
        self.page.locator('#careError').wait_for(state='visible')
        self.assertEqual(self.page.input_value('#profileName'),'新姓名')
        self.assertNotIn('private',self.page.inner_text('#careError'))
        self.fail_write=None;self.page.click('#residentProfileForm button')
        self.page.wait_for_function('() => window.App.currentPatient.name==="新姓名"')
        self.page.click('#residentLogout');self.page.wait_for_url('**/dist/login.html')
        self.assertIsNone(self.user)

    def test_staff_identity_conflict_retry_and_single_staff_escalation(self):
        self.staff()
        self.assertEqual(self.page.input_value('#currentStaff'),'care-nurse')
        self.assertTrue(self.page.is_disabled('#currentStaff'))
        self.assertEqual(self.page.locator('[data-sim]').count(),0)
        self.page.locator('#todoEvents [data-event]').first.click()
        self.page.click('[data-event-action="claim"]');self.page.click('[data-event-action="processing"]')
        self.page.locator('.escalation-options summary').click()
        self.assertTrue(self.page.is_disabled('[data-event-action="escalated"]'))
        self.page.fill('#eventNote','保留这段核实记录');self.fail_write=409
        self.page.click('[data-event-action="resolved"]')
        self.page.locator('#eventError').wait_for(state='visible')
        self.assertEqual(self.page.input_value('#eventNote'),'保留这段核实记录')
        self.fail_write=None;self.snapshot['events'][0]['revision']+=1
        self.page.click('#eventDialog [data-care-retry]')
        self.page.wait_for_function('() => document.querySelector("#careError").hidden')
        self.assertEqual(self.page.input_value('#eventNote'),'保留这段核实记录')
        self.page.click('[data-event-action="resolved"]')
        self.page.wait_for_function('() => document.querySelector("#eventBody").textContent.includes("此事件已结案")')
        mutations=[r[2] for r in self.requests if r[1].endswith('/transitions')]
        self.assertTrue(all('actor' not in value for value in mutations))

    def test_network_recovery_invalid_data_and_session_expiry(self):
        self.resident();self.fail_snapshot='network'
        self.page.evaluate('window.dispatchEvent(new Event("online"))')
        self.page.locator('#careError').wait_for(state='visible')
        self.assertTrue(self.page.locator('#contactForm').evaluate('(node)=>node.inert'))
        self.assertTrue(self.page.locator('a[href="tel:120"]').is_visible())
        self.fail_snapshot=None;self.page.click('#retryCare')
        self.page.wait_for_function('() => document.querySelector("#careError").hidden')
        self.fail_snapshot='invalid';self.page.evaluate('window.dispatchEvent(new Event("online"))')
        self.page.locator('#careError').wait_for(state='visible')
        self.assertIn('服务数据不完整',self.page.inner_text('#careError'))
        self.fail_snapshot=401;self.page.click('#retryCare')
        self.page.locator('dialog:has-text("登录已失效")').wait_for(state='visible')
        self.assertIn('/dist/index.html',self.page.url)

    def test_inquiry_failure_keeps_draft_and_success_only_after_service_accepts(self):
        self.page.goto(self.base+'contact.html')
        self.assertEqual(self.page.locator('a[href="tel:4008231998"]').count(),0)
        self.page.fill('#f-name','测试称呼');self.page.fill('#f-phone','13800000000');self.page.fill('#f-msg','请介绍社区服务流程')
        self.fail_write=503;self.page.click('form [type="submit"]')
        self.page.locator('#messageError').wait_for(state='visible')
        self.assertFalse(self.page.locator('.form-success').is_visible())
        self.assertEqual(self.page.input_value('#f-msg'),'请介绍社区服务流程')
        self.fail_write=None;self.page.click('form [type="submit"]')
        self.page.locator('.form-success').wait_for(state='visible')
        self.assertIsNone(self.page.evaluate('localStorage.getItem("yihe-messages")'))

    def test_release_excludes_demo_assets_and_more_view_renders(self):
        for name in ['data/residents.js','js/seed-data.js','js/config.js','design-preview.html']:
            self.assertFalse((community.ROOT/'dist'/name).exists())
        self.staff();self.page.click('[data-view="more"]')
        self.assertIn('服务账户',self.page.inner_text('#accountDescription'))
        self.assertEqual(self.page.locator('#legacyAudit').count(),0)
        self.page.set_viewport_size({'width':390,'height':844})
        self.assertLessEqual(self.page.evaluate('document.documentElement.scrollWidth'),390)

    def test_local_demo_identity_cannot_open_a_protected_release_page(self):
        self.context.add_init_script('localStorage.setItem("hm-login",JSON.stringify({uid:1,name:"forged",ts:Date.now()}))')
        self.page.goto(self.base+'index.html');self.page.wait_for_url('**/dist/login.html')
        self.assertFalse(any(path.endswith('/care/snapshot') for _,path,_,_ in self.requests))

    def test_missing_server_metric_states_never_show_normal(self):
        self.resident();self.page.locator('.resident-metrics summary').click()
        self.assertEqual(self.page.locator('.vital-card[data-status="unknown"]').count(),4)
        self.assertNotIn('参考范围内',self.page.inner_text('#residentVitals'))


    def test_staff_reauthentication_keeps_unsaved_note(self):
        self.staff();self.page.locator('#todoEvents [data-event]').first.click()
        self.page.click('[data-event-action="claim"]');self.page.click('[data-event-action="processing"]')
        self.page.fill('#eventNote','会话过期前的处置草稿');self.fail_write=401
        self.page.click('[data-event-action="resolved"]')
        self.page.locator('#adminLoginOverlay').wait_for(state='visible')
        self.fail_write=None
        self.page.fill('#adminLoginName','server-staff');self.page.fill('#adminLoginPassword','test-password');self.page.click('#adminLoginSubmit')
        self.page.wait_for_function('() => document.querySelector("#eventDialog").open')
        self.assertEqual(self.page.input_value('#eventNote'),'会话过期前的处置草稿')

    def test_resident_reauthentication_keeps_profile_draft(self):
        self.resident();self.page.click('[data-tab="me"]');self.page.fill('#profileName','未提交姓名')
        self.fail_write=401;self.page.click('#residentProfileForm button')
        dialog=self.page.locator('dialog:has-text("登录已失效")');dialog.wait_for(state='visible')
        self.fail_write=None
        dialog.locator('input[autocomplete="username"]').fill('server-resident')
        dialog.locator('input[type="password"]').fill('test-password');dialog.locator('button').click()
        self.page.wait_for_function('() => !document.querySelector("main").inert')
        self.assertEqual(self.page.input_value('#profileName'),'未提交姓名')

    def test_http_idle_locks_staff_page_without_destroying_note(self):
        self.staff();self.page.locator('#todoEvents [data-event]').first.click()
        self.page.fill('#eventNote','空闲锁定前的草稿')
        self.page.evaluate('''() => {const original=performance.now.bind(performance);performance.now=()=>original()+16*60*1000;window.dispatchEvent(new Event('pageshow'));}''')
        self.page.locator('#adminLoginOverlay').wait_for(state='visible')
        self.assertEqual(self.page.input_value('#eventNote'),'空闲锁定前的草稿')
        self.page.wait_for_function('() => CareSession.current===null')


    def test_another_staff_page_still_presents_after_global_receipt(self):
        self.staff()
        self.assertEqual(self.snapshot['events'][0]['notification']['presentedCycle'],1)
        second=self.context.new_page();second.goto(self.base+'管理端.html')
        second.wait_for_function('() => document.querySelector("#dangerModal").open')
        self.assertTrue(second.locator('#dangerModal').is_visible())
        second.close()
