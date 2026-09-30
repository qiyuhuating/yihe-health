"""Front-end fault handling: real UI outcomes and preserved local records."""
import json
import test_community as community


class ResilienceTests(community.BrowserCase):
    admin = community.CommunityTests.admin
    resident = community.CommunityTests.resident
    nav = community.CommunityTests.nav

    def test_missing_credentials_refuses_login_without_writing_session(self):
        self.context.route('**/demo-credentials.js', lambda route: route.abort())
        self.page.goto(self.url+'login.html')
        self.page.fill('#loginName', '王国栋'); self.page.fill('#loginPassword', '123456')
        self.page.click('#loginSubmit')
        self.page.locator('#loginHint').wait_for(state='visible')
        self.assertIn('登录校验', self.page.inner_text('#loginHint'))
        self.assertTrue(self.page.url.endswith('/login.html'))
        self.assertIsNone(self.page.evaluate('localStorage.getItem("hm-login")'))

    def test_login_rejection_keeps_input_and_allows_retry(self):
        self.page.goto(self.url+'login.html')
        self.page.evaluate('()=>{window.verifyOriginal=DemoCredentials.verify; DemoCredentials.verify=()=>Promise.reject(Error("unavailable"));}')
        self.page.fill('#loginName', '王国栋'); self.page.fill('#loginPassword', '123456')
        self.page.click('#loginSubmit')
        self.page.wait_for_function('()=>!document.querySelector("#loginHint").hidden')
        self.assertIsNone(self.page.evaluate('localStorage.getItem("hm-login")'))
        self.assertEqual(self.page.input_value('#loginName'), '王国栋')
        self.assertTrue(self.page.is_enabled('#loginSubmit'))
        self.page.evaluate('()=>{DemoCredentials.verify=window.verifyOriginal;}')
        self.page.click('#loginSubmit'); self.page.wait_for_url('**/index.html')

    def test_corrupt_staff_records_are_retained_and_retry_restores_the_view(self):
        self.admin()
        original = self.page.evaluate('localStorage.getItem(CareAPI.KEY)')
        broken = json.loads(original)
        broken['events'][0]['residentId'] = 999
        raw = json.dumps(broken, ensure_ascii=False)
        self.page.evaluate('(raw)=>localStorage.setItem(CareAPI.KEY,raw)', raw)
        self.page.reload()
        self.page.locator('#careError').wait_for(state='visible')
        self.assertIn('本机监护记录', self.page.inner_text('#careError'))
        self.assertEqual(self.page.evaluate('localStorage.getItem(CareAPI.KEY)'), raw)
        self.assertTrue(self.page.locator('#view-summary').evaluate('(x)=>x.inert'))
        self.page.evaluate('(raw)=>localStorage.setItem(CareAPI.KEY,raw)', original)
        self.page.click('#retryCare')
        self.page.wait_for_function('()=>document.querySelector("#careError").hidden && !!document.querySelector("#workCounts button")')
        self.assertFalse(self.page.locator('#view-summary').evaluate('(x)=>x.inert'))

    def test_corrupt_resident_data_preserves_emergency_link_and_recovers(self):
        self.resident()
        original = self.page.evaluate('localStorage.getItem(CareAPI.KEY)')
        self.page.evaluate('localStorage.setItem(CareAPI.KEY,"{broken");window.dispatchEvent(new CustomEvent("care:change"))')
        self.page.locator('#careError').wait_for(state='visible')
        self.assertIn('本机监护记录', self.page.inner_text('#careError'))
        self.assertTrue(self.page.locator('#residentProfileForm').evaluate('(x)=>x.inert'))
        self.assertTrue(self.page.locator('a[href="tel:120"]').is_visible())
        self.assertEqual(self.page.evaluate('localStorage.getItem(CareAPI.KEY)'), '{broken')
        self.page.evaluate('(raw)=>localStorage.setItem(CareAPI.KEY,raw)', original)
        self.page.click('#retryCare')
        self.page.wait_for_function('()=>document.querySelector("#careError").hidden')
        self.assertFalse(self.page.locator('#residentProfileForm').evaluate('(x)=>x.inert'))

    def test_missing_measurements_show_unknown_and_keep_health_alert_open(self):
        self.admin(); self.page.evaluate('CareAPI.simulate(1,"health")')
        self.page.evaluate('''async()=>{const s=await CareAPI.snapshot();delete s.residents[1].bloodOxygen;
            s.residents[1].trend.push({at:new Date().toISOString(),heartRate:75});s.revision++;
            localStorage.setItem(CareAPI.KEY,JSON.stringify(s));await CareAPI.scan();}''')
        event = self.page.evaluate('CareAPI.snapshot().then(s=>s.events.find(e=>e.residentId===1&&e.type==="health"))')
        self.assertNotIn('sourceRecoveredAt', event)
        self.assertIn('数据不足', event['detail'])
        self.resident(); self.page.locator('.resident-metrics summary').click()
        oxygen = self.page.locator('.vital-card').filter(has_text='血氧')
        self.assertEqual(oxygen.get_attribute('data-status'), 'unknown')
        self.assertIn('数据不足', oxygen.inner_text())
        self.assertIn('—', oxygen.inner_text())
        self.assertNotIn('演示范围内', oxygen.inner_text())

    def test_unavailable_locks_explains_recovery_without_fake_controls(self):
        self.context.add_init_script('Object.defineProperty(navigator,"locks",{value:undefined,configurable:true})')
        self.page.goto(self.url+'管理端.html')
        self.page.fill('#adminLoginName','admin');self.page.fill('#adminLoginPassword','admin123');self.page.click('#adminLoginSubmit')
        self.page.locator('#careError').wait_for(state='visible')
        self.assertIn('HTTPS', self.page.inner_text('#careError'))
        self.assertTrue(self.page.locator('#view-summary').evaluate('(x)=>x.inert'))
        self.assertTrue(self.page.is_visible('#retryCare'))
        self.assertIsNone(self.page.evaluate('localStorage.getItem(CareAPI.KEY)'))

    def test_audit_corrupt_json_is_reported_and_never_overwritten(self):
        self.admin()
        result = self.page.evaluate('''()=>{localStorage.setItem("hm-audit","{broken");
            return {check:Audit.verify(),write:Audit.log("check"),raw:localStorage.getItem("hm-audit")};}''')
        self.assertFalse(result['check']['ok']);self.assertFalse(result['write']['ok'])
        self.assertEqual(result['raw'], '{broken')

    def test_audit_new_timestamps_are_iso_and_clear_is_recorded_atomically(self):
        self.admin()
        result = self.page.evaluate('''()=>{Audit.log("before");const before=localStorage.getItem("hm-audit");
            const set=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==="hm-audit")throw Error("quota");return set.call(this,k,v)};
            const failed=Audit.clear(),untouched=localStorage.getItem("hm-audit")===before;Storage.prototype.setItem=set;
            const cleared=Audit.clear();return {failed,untouched,cleared,records:Audit.all(),check:Audit.verify()};}''')
        self.assertFalse(result['failed']);self.assertTrue(result['untouched']);self.assertTrue(result['cleared'])
        self.assertEqual(len(result['records']), 1);self.assertTrue(result['check']['ok'])
        self.assertIn('清空', result['records'][0]['action'])
        self.assertRegex(result['records'][0]['ts'], r'^\d{4}-\d{2}-\d{2}T.*Z$')

    def test_cross_tab_older_restored_record_refreshes_list_and_details(self):
        self.admin(); self.nav('residents')
        original = self.page.evaluate('localStorage.getItem(CareAPI.KEY)')
        writer = self.context.new_page(); writer.goto(self.url+'home.html')
        changed = json.loads(original); changed['revision'] += 10; changed['residents']['1']['name'] = '更新姓名'
        writer.evaluate('(s)=>localStorage.setItem("yihe-community-v1",s)', json.dumps(changed, ensure_ascii=False))
        self.page.wait_for_function('()=>document.querySelector("#residentList").textContent.includes("更新姓名")')
        writer.evaluate('(s)=>localStorage.setItem("yihe-community-v1",s)', original)
        self.page.wait_for_function('()=>document.querySelector("#residentList").textContent.includes("王国栋")')
        self.page.locator('#residentList [data-resident="1"]').click()
        self.assertIn('王国栋', self.page.inner_text('#residentTitle'))

    def test_interpolated_resident_and_event_text_stays_text_in_dom_and_export(self):
        self.admin()
        payload = '<img src=x>'
        self.page.evaluate('''async name=>{const before=await CareAPI.snapshot();await CareAPI.saveResident(1,{name},'staff-1',before.residents[1].revision);await CareAPI.simulate(1,'health');
            const s=await CareAPI.snapshot(),e=s.events.find(e=>e.residentId===1&&e.type==='health');
            let current=await CareAPI.transition(e.id,'claim','staff-1','',e.revision);
            current=await CareAPI.transition(e.id,'processing','staff-1','',current.revision);
            await CareAPI.transition(e.id,'resolved','staff-1',name,current.revision);}''', payload)
        if self.page.locator('#dangerModal').evaluate('(x)=>x.open'): self.page.click('#dismissNewAlerts')
        self.nav('residents'); self.page.locator('#residentList [data-resident="1"]').click()
        self.assertIn(payload, self.page.inner_text('#residentTitle'))
        self.assertEqual(self.page.locator('#residentBody img').count(), 0)
        self.page.locator('#residentDialog [aria-label="关闭居民详情"]').click()
        self.nav('more')
        with self.page.expect_download() as download: self.page.click('#exportEvents')
        with open(download.value.path(), encoding='utf-8') as f: exported = json.load(f)
        event = next(e for e in exported['events'] if e['residentId'] == 1 and e['type'] == 'health')
        self.assertEqual(event['residentName'], payload)
        self.assertEqual(event['history'][-1]['note'], payload)

    def test_failed_simulator_toggle_restores_saved_value(self):
        self.admin(); self.nav('more')
        self.assertFalse(self.page.is_checked('#simToggle'))
        self.page.evaluate('''()=>{const set=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){
            if(k===CareAPI.KEY)throw Error('quota');return set.call(this,k,v)};}''')
        self.page.locator('#simToggle').click()
        self.page.locator('#careError').wait_for(state='visible')
        self.assertFalse(self.page.is_checked('#simToggle'))
        self.assertFalse(self.page.evaluate('JSON.parse(localStorage.getItem(CareAPI.KEY)).simulator'))
