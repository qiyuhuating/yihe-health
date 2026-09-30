"""Browser-to-real-HTTP-to-SQLite acceptance; no intercepted API responses."""
import json
import sys
import tempfile
import threading
import urllib.request
from pathlib import Path
sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
from backend.service import CareApplication, CareStore, ThreadedServer, QuietHandler
from wsgiref.simple_server import make_server
from test_community import BrowserCase, ROOT


class LiveBackendTests(BrowserCase):
    def setUp(self):
        super().setUp()
        self.temp = tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.store = CareStore(Path(self.temp.name)/'care.sqlite3')
        self.store.add_user('live-staff', 'staff', 'staff-1', '合成工作人员', 'test-password-123')
        self.store.add_resident({'id': 1, 'name': '合成居民', 'age': 70, 'responsible': 'staff-1'})
        self.store.add_user('live-resident', 'resident', 1, '合成居民账号', 'test-password-456')
        self.app = CareApplication(self.store, 'http://127.0.0.1:8000', ROOT/'dist', secure=False, device_key='synthetic-device-key')
        self.server_live = make_server('127.0.0.1', 0, self.app, server_class=ThreadedServer, handler_class=QuietHandler)
        self.base = 'http://127.0.0.1:'+str(self.server_live.server_port)+'/'
        self.app.origin = self.base.rstrip('/')
        self.thread_live = threading.Thread(target=self.server_live.serve_forever, daemon=True); self.thread_live.start()
        self.addCleanup(self.close_server)

    def close_server(self):
        self.server_live.shutdown();self.server_live.server_close();self.thread_live.join(timeout=2)

    def device(self, key='signal-1', active=True, severity='danger'):
        req = urllib.request.Request(self.base+'api/v1/device/events', method='POST', data=json.dumps({'residentId': 1, 'type': 'health', 'active': active, 'severity': severity, 'detail': '真实 HTTP 合成危险输入', 'metrics': {'bloodOxygen': 80}, 'metricStates': {'bloodOxygen': 'danger'}}).encode(), headers={'Content-Type': 'application/json', 'Authorization': 'Bearer synthetic-device-key', 'Idempotency-Key': key})
        with urllib.request.urlopen(req) as response: self.assertEqual(response.status, 204)

    def staff(self):
        self.page.goto(self.base+'管理端.html'); self.page.fill('#adminLoginName', 'live-staff');self.page.fill('#adminLoginPassword', 'test-password-123');self.page.click('#adminLoginSubmit')
        self.page.wait_for_function('() => !!document.querySelector("#workCounts button")')

    def test_device_danger_to_page_disposition_and_persistent_audit(self):
        self.device();self.staff()
        self.page.wait_for_function('() => document.querySelector("#dangerModal").open');self.page.click('#viewNewAlerts')
        self.page.locator('#allEvents [data-event]').first.click()
        self.page.click('[data-event-action="claim"]');self.page.click('[data-event-action="processing"]');self.page.fill('#eventNote', '核实后记录合成处置')
        self.page.click('[data-event-action="resolved"]')
        self.page.wait_for_function('() => document.querySelector("#eventBody").textContent.includes("此事件已结案")')
        with self.store.connect() as db:
            events=[json.loads(r['data']) for r in db.execute('SELECT data FROM care_events')]
            self.assertEqual(len(events), 2); self.assertTrue(any(e['state']=='new' for e in events))
            self.assertEqual(db.execute('SELECT COUNT(*) FROM audit_logs').fetchone()[0], 4)
            self.assertEqual(db.execute("SELECT COUNT(*) FROM notification_outbox WHERE channel='sms' AND status='disabled'").fetchone()[0], 2)

    def test_real_resident_revision_conflict_preserves_draft(self):
        self.page.goto(self.base+'login.html');self.page.fill('#loginName', 'live-resident');self.page.fill('#loginPassword', 'test-password-456');self.page.click('#loginSubmit')
        self.page.wait_for_function('() => window.App?.USER_ID===1');self.page.click('[data-tab="me"]');self.page.fill('#profileName','本页未提交姓名')
        # A separate server transaction represents another editor, not a fake HTTP response.
        with self.store.transaction() as db:
            p=self.store.resident(db,1);p['revision']+=1;p['name']='其他页面已保存';self.store.save(db,'residents',1,p);self.store.revision(db)
        self.page.click('#residentProfileForm button[type="submit"]')
        self.page.locator('#residentProfileForm [data-form-conflict]').wait_for(state='visible')
        self.assertEqual(self.page.input_value('#profileName'),'本页未提交姓名')

    def test_live_adapters_validate_snapshot_and_do_not_persist_health_data(self):
        self.staff();self.device();self.page.click('#refreshCare')
        self.page.wait_for_function('() => document.querySelector("#dangerModal").open')
        self.assertIsNone(self.page.evaluate('localStorage.getItem("yihe-community-v1")'))
        snapshot=self.page.evaluate('() => CareAPI.snapshot()')
        self.assertEqual(snapshot['events'][0]['severity'],'danger');self.assertIn('serverTime',snapshot)


    def test_same_contract_scenarios_on_demo_and_real_http(self):
        scenario = r"""async () => {
          const api=CareAPI,actor='staff-1',rid=1;let s=await api.snapshot(),p=s.residents[rid];
          await api.saveResident(rid,{name:'契约测试姓名'},actor,p.revision);
          let conflict;try{await api.saveResident(rid,{age:71},actor,p.revision);}catch(e){conflict=e.code;}
          p=(await api.snapshot()).residents[rid];
          await api.saveResident(rid,{contacts:[{name:'甲',phone:'12345'},{name:'乙',phone:'54321'}]},actor,p.revision);
          p=(await api.snapshot()).residents[rid];
          await api.saveResident(rid,{contactUpdates:[{index:0,value:{name:'丙',phone:'67890'}}]},actor,p.revision);
          const due=new Date(api.now()+3600000).toISOString();
          await api.addDose(rid,'合成契约任务',due,actor,'same-intent');await api.addDose(rid,'合成契约任务',due,actor,'same-intent');
          p=(await api.snapshot()).residents[rid];return {conflict,name:p.name,contact:p.contacts[1].name,doses:p.doses.filter(d=>d.name==='合成契约任务').length};
        }"""
        self.staff();http_result=self.page.evaluate(scenario)
        demo=self.context.new_page();demo.goto(self.url+'管理端.html')
        demo.fill('#adminLoginName','admin');demo.fill('#adminLoginPassword','admin123');demo.click('#adminLoginSubmit')
        demo.wait_for_function('() => !!document.querySelector("#workCounts button")')
        demo_result=demo.evaluate(scenario);demo.close()
        expected={'conflict':'CONFLICT','name':'契约测试姓名','contact':'乙','doses':1}
        self.assertEqual(http_result,expected);self.assertEqual(demo_result,expected)
