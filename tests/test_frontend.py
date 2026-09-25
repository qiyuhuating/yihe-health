"""Run: python -m unittest discover -s tests -v. Uses an isolated local browser."""
import functools
import http.server
import json
import pathlib
import threading
import unittest
from playwright.sync_api import sync_playwright

ROOT = pathlib.Path(__file__).resolve().parents[1]

class QuietHandler(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass

class FrontendTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), functools.partial(QuietHandler, directory=str(ROOT)))
        threading.Thread(target=cls.server.serve_forever, daemon=True).start()
        cls.url = f'http://127.0.0.1:{cls.server.server_port}/'
        cls.pw = sync_playwright().start()
        cls.browser = cls.pw.chromium.launch()

    @classmethod
    def tearDownClass(cls):
        cls.browser.close()
        cls.pw.stop()
        cls.server.shutdown()
        cls.server.server_close()

    def setUp(self):
        self.context = self.browser.new_context(viewport={'width':390,'height':844}, timezone_id='Asia/Shanghai')
        self.page = self.context.new_page()
        self.page.set_default_timeout(5000)
        self.errors = []
        self.page.on('pageerror', lambda e: self.errors.append(str(e)))
        # Never send test inputs to an external AI provider.
        self.context.route('https://api.deepseek.com/**', lambda r: r.abort())

    def tearDown(self):
        self.context.close()

    def go(self, path):
        self.page.goto(self.url + path)

    def login(self):
        self.go('login.html')
        self.page.fill('#loginName','王国栋')
        self.page.fill('#loginPassword','123456')
        self.page.click('#loginSubmit')
        self.page.wait_for_url('**/index.html*')
        self.page.wait_for_function('() => !!App.currentPatient')

    def admin(self):
        self.go('管理端.html')
        self.page.fill('#adminLoginName','admin')
        self.page.fill('#adminLoginPassword','admin123')
        self.page.click('#adminLoginSubmit')
        self.page.wait_for_function('() => document.getElementById("adminLoginOverlay").hidden')
        self.page.evaluate('API.setEnabled(false); AudioUtils.startAlarm=()=>{}; AudioUtils.speak=()=>{}')

    def test_content_visible_search_and_detail(self):
        self.go('yangheng.html')
        self.page.wait_for_function('() => document.querySelectorAll(".ch-card").length===24')
        self.assertEqual(self.page.locator('.ch-card').first.evaluate('(e)=>getComputedStyle(e).opacity'),'1')
        self.page.fill('#chSearch','秋')
        self.page.wait_for_function('() => document.querySelectorAll(".ch-card").length===4')
        self.page.locator('.ch-card').first.click()
        self.assertEqual(self.page.locator('#detail').get_attribute('aria-hidden'),'false')
        self.page.keyboard.press('Escape')
        self.assertEqual(self.page.locator('#detail').get_attribute('aria-hidden'),'true')
        self.assertEqual(self.errors,[])

    def test_content_without_animation_observer(self):
        self.page.add_init_script('window.IntersectionObserver=undefined')
        self.context.set_default_timeout(5000)
        self.page.emulate_media(reduced_motion='reduce')
        self.go('yangheng.html')
        self.page.wait_for_function('() => document.querySelectorAll(".ch-card").length===24')
        self.assertEqual(self.page.locator('.ch-card').first.evaluate('(e)=>getComputedStyle(e).opacity'),'1')
        self.assertEqual(self.errors,[])

    def test_narrow_home_and_admin(self):
        self.login()
        for width in [320,390,768,1440]:
            self.page.set_viewport_size({'width':width,'height':844})
            self.assertLessEqual(self.page.evaluate('document.documentElement.scrollWidth'),width)
        self.page.set_viewport_size({'width':390,'height':844})
        self.admin()
        self.assertLessEqual(self.page.evaluate('document.documentElement.scrollWidth'),390)

    def test_toast_does_not_block_navigation(self):
        self.login()
        self.page.evaluate('showToast("资料已保存到当前账号的本机记录")')
        self.page.click('[data-tab="health"]',timeout=1200)
        self.assertTrue(self.page.locator('#tab-health').is_visible())

    def test_ai_config_failure_keeps_dialog_open(self):
        self.login()
        self.page.click('[data-tab="me"]')
        self.page.click('[data-action="aiSettings"]')
        self.page.fill('#aiApiKey','test-placeholder-not-a-real-key')
        self.page.check('#aiPersistKey')
        self.page.evaluate('DataStore.set=async()=>false')
        self.page.click('#btnSaveAI')
        self.page.wait_for_function('() => document.querySelector(".error-toast")')
        self.assertTrue(self.page.locator('#aiOverlay').is_visible())
        self.assertFalse(self.page.evaluate('!!AIChat.getConfig().hasKey'))
        self.assertIn('失败',self.page.locator('.error-toast').last.inner_text())
        self.assertEqual(self.errors,[])

    def test_chat_fallback_survives_reload(self):
        self.login()
        self.page.evaluate('DataStore.saveChat=async()=>false;DataStore.saveChats=async()=>false')
        self.page.click('[data-tab="chat"]')
        self.page.locator('.msg-ct').first.click()
        self.page.fill('#chatInput','persistence-test-message')
        self.page.click('#chatSend')
        self.page.wait_for_function('() => Object.keys(localStorage).some(k=>k.startsWith("hm-chat-v2"))')
        self.page.reload()
        self.page.wait_for_function('() => document.querySelector(".msg-ct")')
        self.page.locator('.msg-ct').first.click()
        self.assertIn('persistence-test-message',self.page.locator('#chatBody').inner_text())

    def test_chat_with_indexeddb_unavailable(self):
        self.page.add_init_script("IDBFactory.prototype.open=function(){throw new Error('Test: IndexedDB unavailable')}")
        self.login()
        self.page.click('[data-tab="chat"]')
        self.page.locator('.msg-ct').first.click()
        self.page.fill('#chatInput','idb-unavailable-message')
        self.page.click('#chatSend')
        self.page.wait_for_function('() => (localStorage.getItem(userKey(KEYS.CHAT_V2,App.USER_ID))||"").includes("idb-unavailable-message")')
        self.page.reload()
        self.page.wait_for_function('() => document.querySelector(".msg-ct")')
        self.page.locator('.msg-ct').first.click()
        self.assertIn('idb-unavailable-message',self.page.locator('#chatBody').inner_text())
        self.assertEqual(self.errors,[])

    def test_all_chat_storage_failure_is_visible(self):
        self.login()
        self.page.evaluate('''() => {
            DataStore.saveChats=async()=>false;
            const original=Storage.prototype.setItem;
            Storage.prototype.setItem=function(k,v){if(k.startsWith(KEYS.CHAT_V2))throw new DOMException('quota','QuotaExceededError');return original.call(this,k,v)};
        }''')
        self.page.click('[data-tab="chat"]')
        self.page.locator('.msg-ct').first.click()
        self.page.fill('#chatInput','unsaved-message')
        self.page.click('#chatSend')
        self.page.wait_for_function('() => [...document.querySelectorAll(".error-toast")].some(x=>x.textContent.includes("聊天记录保存失败"))')
        self.assertIn('unsaved-message',self.page.locator('#chatBody').inner_text())
        self.assertEqual(self.errors,[])

    def test_ai_remember_restore_clear(self):
        self.login()
        self.page.evaluate("async()=>{await AIChat.ready();await AIChat.saveConfig('test-not-a-real-key','doctor','',true,true)}")
        stored=self.page.evaluate("async()=>DataStore.get('ai-config-'+App.USER_ID)")
        self.assertTrue(stored['_key'].startswith('aes:'))
        self.assertNotIn('test-not-a-real-key',json.dumps(stored))
        self.page.reload()
        self.page.evaluate('async()=>{await AIChat.ready()}')
        self.assertTrue(self.page.evaluate('AIChat.getConfig().hasKey'))
        self.page.evaluate('async()=>{await AIChat.clear()}')
        self.page.reload()
        self.page.evaluate('async()=>{await AIChat.ready()}')
        self.assertFalse(self.page.evaluate('AIChat.getConfig().hasKey'))
        self.assertEqual(self.errors,[])

    def test_report_download_and_calendar_boundary(self):
        self.login()
        self.page.click('[data-tab="health"]')
        self.page.click('#genWeekTab')
        self.assertIn('有效样本：',self.page.locator('#reportContentTab').inner_text())
        with self.page.expect_download() as download:
            self.page.click('#btnDownloadReport')
        self.assertEqual(download.value.suggested_filename,'演示健康摘要.txt')
        result=self.page.evaluate('''()=>ReportMath.aggregate([
            {at:'2026-09-20T15:59:59Z',hr:100},
            {at:'2026-09-20T16:00:00Z',hr:70},
            {at:'2026-09-25T07:00:00Z',hr:100}
        ],'week',new Date('2026-09-25T06:00:00Z'))''')
        self.assertEqual(result['count'],1)
        self.assertEqual(result['hr'],70)

    def test_reminder_and_profile_isolation(self):
        self.login()
        self.page.click('[data-home-action="medReminder"]')
        self.page.fill('#taskText','本机测试提醒')
        self.page.click('#saveMedTimes')
        self.assertIn('本机测试提醒',self.page.locator('#taskList').inner_text())
        self.page.click('#cancelMedTimes')
        self.page.click('[data-tab="me"]')
        self.page.click('[data-action="editProfile"]')
        for selector,value in [('#editName','甲用户'),('#editAge','75'),('#editHeight','160'),('#editWeight','60')]:
            self.page.fill(selector,value)
        self.page.click('#btnSaveProfile')
        self.page.click('[data-action="switchUser"]')
        self.page.wait_for_url('**/login.html')
        self.page.fill('#loginName','张德福');self.page.fill('#loginPassword','123456');self.page.click('#loginSubmit')
        self.page.wait_for_function('() => window.App?.currentPatient?.name==="张德福"')
        self.page.click('[data-tab="me"]')
        self.assertEqual(self.page.locator('#meName').inner_text(),'张德福')
        self.assertEqual(self.page.evaluate('DemoDomain.reminders(App.USER_ID)'),[])

    def test_public_pages_and_privacy(self):
        for path in ['home.html','about.html','services.html','news.html','guide.html','contact.html','privacy.html']:
            self.go(path)
            self.assertTrue(self.page.locator('.site-demo-notice').is_visible())
            self.assertEqual(self.page.locator('a[href="privacy.html"]').count(),1)
            self.assertNotIn('1800XXXX',self.page.locator('body').inner_text())
        self.assertIn('IndexedDB',self.page.locator('main').inner_text())
        self.assertEqual(self.errors,[])

    def test_contact_failure_does_not_claim_success(self):
        self.go('contact.html')
        self.page.locator('input[name="name"]').fill('演示访客')
        self.page.locator('input[name="phone"]').fill('13800000000')
        self.page.locator('textarea[name="message"]').fill('仅测试本机表单')
        self.page.evaluate("() => {Storage.prototype.setItem=function(){throw new DOMException('quota','QuotaExceededError')}}")
        messages=[]
        self.page.on('dialog',lambda d:(messages.append(d.message),d.accept()))
        self.page.locator('form button[type="submit"]').click()
        self.assertTrue(any('保存失败' in x for x in messages))
        self.assertFalse(self.page.locator('.form-success').is_visible())

    def test_alert_acknowledgement_and_recurrence(self):
        self.admin()
        result=self.page.evaluate('''async()=>{
            Math.random=()=>0.5;
            await API.simulateDangerFor(1);
            const first=(await API.getAlerts()).find(x=>x.id===1);
            await API.acknowledgeAlert(first.eventId);
            const ack=(await API.getAlerts()).find(x=>x.id===1);
            await API.simulateDangerFor(1);
            const next=(await API.getAlerts()).find(x=>x.id===1);
            return {first,ack,next};
        }''')
        self.assertEqual(result['ack']['state'],'acknowledged')
        self.assertEqual(result['next']['state'],'triggered')
        self.assertNotEqual(result['first']['eventId'],result['next']['eventId'])

    def test_alert_escape_and_next_event(self):
        self.admin()
        self.page.evaluate('''async()=>{await API.simulateDangerFor(1);await AdminApp.refreshSummary()}''')
        self.page.wait_for_function('() => !document.getElementById("dangerModal").hidden')
        self.assertTrue(self.page.locator('#btnSendSMS').is_disabled())
        self.page.locator('#btnAcknowledge').focus()
        self.page.keyboard.press('Escape')
        self.page.wait_for_function('() => document.getElementById("dangerModal").hidden && !AlertSystem._current')
        self.page.evaluate('''async()=>{await API.simulateDangerFor(2);await AdminApp.refreshSummary()}''')
        self.page.wait_for_function('() => !document.getElementById("dangerModal").hidden && AlertSystem._current.id===2')
        self.assertEqual(self.errors,[])

    def test_alert_failure_keeps_modal(self):
        self.admin()
        self.page.evaluate('async()=>{await API.simulateDangerFor(1);await AdminApp.refreshSummary()}')
        self.page.wait_for_function('() => !document.getElementById("dangerModal").hidden')
        self.page.evaluate('''()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==='yihe-alert-ack-v3')throw new DOMException('quota','QuotaExceededError');return original.call(this,k,v)}}''')
        self.page.click('#btnAcknowledge')
        self.page.wait_for_function('() => !!document.querySelector(".error-toast")')
        self.assertTrue(self.page.locator('#dangerModal').is_visible())
        self.assertIsNotNone(self.page.evaluate('AlertSystem._current'))
        self.assertEqual(self.errors,[])

if __name__=='__main__':
    unittest.main()
