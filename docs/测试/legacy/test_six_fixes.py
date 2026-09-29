"""Regression tests for admissions, sync, pagination, audit, masking and theme."""
import unittest
import test_frontend as base


class SixFixTests(unittest.TestCase):
    setUpClass = classmethod(base.FrontendTests.setUpClass.__func__)
    tearDownClass = classmethod(base.FrontendTests.tearDownClass.__func__)
    setUp = base.FrontendTests.setUp
    tearDown = base.FrontendTests.tearDown
    admin = base.FrontendTests.admin
    login = base.FrontendTests.login
    go = base.FrontendTests.go

    def view(self, name):
        self.page.click('#hamburgerBtn')
        self.page.click('[data-view="'+name+'"]')

    def test_admission_storage_failure_is_atomic(self):
        self.admin()
        result = self.page.evaluate('''async () => {
            const original=Storage.prototype.setItem;
            Storage.prototype.setItem=function(k,v){if(k===KEYS.ADMISSIONS)throw Error('disk full');return original.call(this,k,v)};
            let rejected=false;
            try {await API.admitPatient(1,{hospital:'测试医院',admissionDate:'2026-01-01',reason:'观察'})} catch(e){rejected=true}
            return {rejected,admitted:(await API.getPatientDetail(1)).admitted,records:loadJSON(KEYS.ADMISSIONS,[])};
        }''')
        self.assertEqual(result, {'rejected':True,'admitted':False,'records':[]})

    def test_storage_unavailable_keeps_patient_reads_usable(self):
        self.page.add_init_script('''Storage.prototype.setItem=function(){throw Error('storage unavailable')}''')
        self.go('管理端.html')
        result=self.page.evaluate('''async()=>{
            const count=(await API.getPatients()).length;
            let rejected=false;
            try{await API.admitPatient(1,{hospital:'测试医院',admissionDate:'2026-01-01'})}catch(e){rejected=true}
            return {count,rejected,admitted:(await API.getPatientDetail(1)).admitted};
        }''')
        self.assertEqual(result,{'count':12,'rejected':True,'admitted':False})

    def test_admit_discharge_reload_validation_and_failure(self):
        self.admin()
        result = self.page.evaluate('''async () => {
            const data={hospital:'测试医院',admissionDate:'2026-01-01',reason:'观察'};
            const admitted=(await API.admitPatient(1,data)).record;
            let repeated=false, invalid=false, missingSummary=false, invalidCalendar=false, future=false, tooEarly=false;
            try{await API.admitPatient(1,data)}catch(e){repeated=true}
            for(const [date,flag] of [['2026-02-31','invalidCalendar'],['2999-01-01','future'],['1899-12-31','tooEarly']]){
                try{await API.admitPatient(2,{...data,admissionDate:date})}catch(e){if(flag==='invalidCalendar')invalidCalendar=true;if(flag==='future')future=true;if(flag==='tooEarly')tooEarly=true}
            }
            try{await API.dischargePatient(1,{dischargeDate:'2025-12-31',dischargeSummary:'康复'})}catch(e){invalid=true}
            try{await API.dischargePatient(1,{dischargeDate:'2026-01-02'})}catch(e){missingSummary=true}
            const during={detail:(await API.getPatientDetail(1)).admitted,list:(await API.getPatients()).find(p=>p.id===1).admitted,locations:(await API.getLocations()).some(p=>p.id===1)};
            const original=Storage.prototype.setItem;
            Storage.prototype.setItem=function(k,v){if(k===KEYS.ADMISSIONS)throw Error('disk full');return original.call(this,k,v)};
            let failed=false;try{await API.dischargePatient(1,{dischargeDate:'2026-01-02',dischargeSummary:'康复'})}catch(e){failed=true}
            Storage.prototype.setItem=original;
            const stillAdmitted=(await API.getPatientDetail(1)).admitted;
            const discharged=(await API.dischargePatient(1,{dischargeDate:'2026-01-02',dischargeSummary:'康复'})).record;
            return {admitted:admitted.status,repeated,invalid,missingSummary,invalidCalendar,future,tooEarly,during,failed,stillAdmitted,discharged:discharged.status,records:loadJSON(KEYS.ADMISSIONS,[]).length,after:(await API.getPatientDetail(1)).admitted};
        }''')
        self.assertEqual(result, {'admitted':'admitted','repeated':True,'invalid':True,'missingSummary':True,'invalidCalendar':True,'future':True,'tooEarly':True,'during':{'detail':True,'list':True,'locations':False},'failed':True,'stillAdmitted':True,'discharged':'discharged','records':1,'after':False})
        self.page.reload()
        self.page.wait_for_function('()=>window.AdminApp && typeof AdminApp.refreshSummary==="function"')
        self.assertFalse(self.page.evaluate('''async()=> (await API.getPatientDetail(1)).admitted'''))

    def test_admission_page_success_and_failed_save_keep_form(self):
        self.admin()
        self.view('admission')
        self.page.select_option('#admissionPatient','1')
        self.page.fill('#admissionHospital','测试医院')
        self.page.fill('#admissionReason','观察')
        self.page.evaluate('''()=>{window.__originalSetItem=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k===KEYS.ADMISSIONS)throw Error('disk full');return window.__originalSetItem.call(this,k,v)}}''')
        self.page.click('#btnAdmit')
        self.page.wait_for_function('()=>document.querySelector("#admissionHint").textContent.includes("失败")')
        self.assertEqual(self.page.locator('#admissionHospital').input_value(),'测试医院')
        self.assertFalse(self.page.evaluate('''async()=> (await API.getPatientDetail(1)).admitted'''))
        self.page.evaluate('()=>{Storage.prototype.setItem=window.__originalSetItem}')
        self.page.click('#btnAdmit')
        self.page.wait_for_function('()=>document.querySelector("#admissionHint").textContent.includes("已入院")')
        self.assertEqual(self.page.locator('#admissionHospital').input_value(),'')
        self.assertTrue(self.page.evaluate('''async()=> (await API.getPatientDetail(1)).admitted'''))
        self.view('discharge')
        self.page.click('.admitted-card[data-pid="1"]')
        self.page.fill('#dischargeSummary','康复')
        self.page.click('#btnDischarge')
        self.page.wait_for_function('()=>document.querySelector("#dischargeHint").textContent.includes("已出院")')
        self.assertFalse(self.page.evaluate('''async()=> (await API.getPatientDetail(1)).admitted'''))
        self.assertIn('王国栋',self.page.locator('#dischargeRecords').inner_text())

    def test_list_and_detail_follow_external_snapshot(self):
        self.admin()
        self.view('profile')
        result = self.page.evaluate('''async () => {
            await API.getPatients();
            const updated=new Promise(resolve=>API.onExternalSync(async()=>{
                const detail=await API.getPatientDetail(1);
                if(detail.heartRate===88)resolve({list:(await API.getPatients()).find(p=>p.id===1).heartRate,detail:detail.heartRate});
            }));
            const data=JSON.parse(localStorage.getItem('cw_sim_store_v2'));
            data.from='regression-other-page';data.v=Date.now();data.patients.find(p=>p.id===1).heartRate=88;
            localStorage.setItem('cw_sim_store_v2',JSON.stringify(data));
            return await updated;
        }''')
        self.assertEqual(result, {'list':88,'detail':88})

    def test_two_tabs_do_not_restore_discharged_state(self):
        self.admin()
        second=self.context.new_page()
        second.goto(self.url+'管理端.html')
        if not second.locator('#adminLoginOverlay').is_hidden():
            second.fill('#adminLoginName','admin')
            second.fill('#adminLoginPassword','admin123')
            second.click('#adminLoginSubmit')
            second.wait_for_function('()=>document.querySelector("#adminLoginOverlay").hidden')
        second.evaluate('''()=>{window.__syncCount=0;API.onExternalSync(()=>window.__syncCount++)}''')
        self.page.evaluate('''async()=>{
            await API.admitPatient(1,{hospital:'测试医院',admissionDate:'2026-01-01',reason:'观察'});
        }''')
        second.wait_for_function('''async()=> (await API.getPatientDetail(1)).admitted''')
        second.evaluate('window.__syncCount=0')
        self.page.evaluate('''async()=>{
            await API.dischargePatient(1,{dischargeDate:'2026-01-02',dischargeSummary:'康复'});
            const old=JSON.parse(localStorage.getItem('cw_sim_store_v2'));
            old.from='stale-other-tab';old.v=Date.now();old.admitted={'1':true};
            localStorage.setItem('cw_sim_store_v2',JSON.stringify(old));
        }''')
        second.wait_for_function('()=>window.__syncCount>0')
        second.wait_for_function('''async()=>{
            const detail=await API.getPatientDetail(1);
            const list=(await API.getPatients()).find(p=>p.id===1);
            return !detail.admitted&&!list.admitted;
        }''')
        self.assertEqual(self.errors,[])

    def test_admission_cross_tab_without_sim_snapshot(self):
        self.admin()
        second=self.context.new_page()
        second.goto(self.url+'管理端.html')
        if not second.locator('#adminLoginOverlay').is_hidden():
            second.fill('#adminLoginName','admin')
            second.fill('#adminLoginPassword','admin123')
            second.click('#adminLoginSubmit')
            second.wait_for_function('()=>document.querySelector("#adminLoginOverlay").hidden')
        second.evaluate('API.setEnabled(false)')
        second.click('#hamburgerBtn')
        second.click('[data-view="admission"]')
        self.page.evaluate('''async()=>{
            const original=Storage.prototype.setItem;
            Storage.prototype.setItem=function(k,v){if(k==='cw_sim_store_v2')throw Error('disk full');return original.call(this,k,v)};
            await API.admitPatient(1,{hospital:'测试医院',admissionDate:'2026-01-01',reason:'观察'});
        }''')
        second.wait_for_function('()=>document.querySelector("#admissionRecords").textContent.includes("王国栋")')
        self.assertTrue(second.evaluate('''async()=> (await API.getPatientDetail(1)).admitted'''))
        self.assertEqual(self.errors,[])

    def test_browse_pagination(self):
        self.admin()
        self.page.evaluate('''async()=>{const p=(await API.getPatients())[0];API.getPatients=async()=>Array.from({length:30},(_,i)=>({...p,id:i+1,name:'测试'+i}));}''')
        self.view('browse')
        self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===24')
        self.page.click('#browseGrid button[data-page="1"]')
        self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===6')
        self.assertTrue(self.page.locator('#browseGrid button').last.is_disabled())
        self.assertEqual(self.errors, [])

    def test_browse_sizes_filter_and_empty(self):
        self.admin()
        self.view('browse')
        for size in (12,24,25,30):
            self.page.evaluate('''async size=>{const p=(await API.getPatients())[0];API.getPatients=async()=>Array.from({length:size},(_,i)=>({...p,id:i+1,name:'测试'+i}));AdminApp.renderBrowseView();}''',size)
            self.page.wait_for_function('size=>document.querySelectorAll(".browse-card").length===Math.min(size,24)',arg=size)
            self.assertEqual(self.page.locator('#browseGrid button[data-page]').count(),0 if size<=24 else 2)
            if size>24:
                self.page.click('#browseGrid button[data-page="1"]')
                self.page.wait_for_function('size=>document.querySelectorAll(".browse-card").length===size-24',arg=size)
                self.assertTrue(self.page.locator('#browseGrid button[data-page="2"]').is_disabled())
                self.page.fill('#browseSearch','不存在')
                self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===0')
                self.assertEqual(self.page.locator('#browseGrid button[data-page]').count(),0)
                self.page.fill('#browseSearch','')
                self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===24')

    def test_audit_rotation(self):
        self.admin()
        result=self.page.evaluate('''()=>{Audit.clear();for(let i=0;i<501;i++)Audit.log('测试',String(i));return {length:Audit.all().length,verified:Audit.verify().ok}}''')
        self.assertEqual(result, {'length':500,'verified':True})

    def test_audit_boundaries_legacy_tampering_and_failed_write(self):
        self.admin()
        result=self.page.evaluate('''()=>{
            Audit.clear();let sizes=[];
            for(let i=1;i<=1000;i++){
                Audit.log('测试',String(i));
                if([499,500,501,1000].includes(i))sizes.push([Audit.all().length,Audit.verify().ok]);
            }
            const retainedOnly=Audit.verify().retainedOnly;
            const stored=JSON.parse(localStorage.getItem('hm-audit'));
            stored.records[0].detail='篡改';localStorage.setItem('hm-audit',JSON.stringify(stored));
            const detected=!Audit.verify().ok;
            Audit.clear();Audit.log('旧记录','一');Audit.log('旧记录','二');
            const legacy=Audit.all();localStorage.setItem('hm-audit',JSON.stringify(legacy));
            const migrated=Audit.log('新记录','三');const oldPreserved=Audit.all().length===3&&Audit.verify().ok&&Audit.verify().retainedOnly;
            const original=Storage.prototype.setItem;
            Storage.prototype.setItem=function(k,v){if(k==='hm-audit')throw Error('disk full');return original.call(this,k,v)};
            const failed=!Audit.log('失败','不应写入').ok;
            Storage.prototype.setItem=original;
            return {sizes,retainedOnly,detected,migrated:migrated.ok,oldPreserved,failed,count:Audit.all().length};
        }''')
        self.assertEqual(result, {'sizes':[[499,True],[500,True],[500,True],[500,True]],'retainedOnly':True,'detected':True,'migrated':True,'oldPreserved':True,'failed':True,'count':3})

    def test_export_choices_mask_names(self):
        self.admin()
        self.view('profile')
        self.page.locator('#settingMask').locator('..').click()
        self.view('sharing')
        self.page.wait_for_function('()=>document.querySelectorAll("#exportResidents input").length===12')
        self.assertNotIn('王国栋', self.page.locator('#exportResidents').inner_text())
        self.assertIn('王*栋', self.page.locator('#exportResidents').inner_text())

    def test_name_mask_across_admin_views_and_alert(self):
        self.admin()
        self.view('profile')
        self.page.locator('#settingMask').locator('..').click()
        self.view('summary')
        self.page.wait_for_function('()=>document.querySelector("#patientTableBody").textContent.includes("王*栋")')
        self.assertNotIn('王国栋',self.page.locator('#patientTableBody').inner_text())
        self.view('browse')
        self.page.wait_for_function('()=>document.querySelector("#browseGrid").textContent.includes("王*栋")')
        self.assertEqual(self.page.evaluate('''async()=> (await API.getPatientDetail(1)).name'''),'王国栋')
        self.page.fill('#browseSearch','王国栋')
        self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===1')
        self.page.fill('#browseSearch','')
        self.page.wait_for_function('()=>document.querySelectorAll(".browse-card").length===12')
        self.page.locator('.browse-card[data-pid="1"]').click()
        self.page.wait_for_function('()=>document.querySelector("#panelBody .hero-name")')
        self.assertEqual(self.page.locator('#panelBody .hero-name').inner_text(),'王*栋')
        self.page.click('#panelClose')
        self.view('map')
        self.page.wait_for_function('()=>document.querySelector("#mapContainer").textContent.includes("王*栋")')
        self.assertNotIn('王国栋',self.page.locator('#mapContainer').inner_text())
        self.page.evaluate('''async()=>{await API.admitPatient(2,{hospital:'测试医院',admissionDate:'2026-01-01',reason:'观察'})}''')
        self.view('admission')
        self.assertIn('张*福',self.page.locator('#admissionRecords').inner_text())
        self.view('discharge')
        self.assertIn('张*福',self.page.locator('#admittedList').inner_text())
        self.page.evaluate('''async()=>{await API.simulateDangerFor(1);await AdminApp.refreshSummary()}''')
        self.view('summary')
        self.page.evaluate('''async()=>await AdminApp.refreshSummary()''')
        self.assertNotIn('王国栋',self.page.locator('#alertList').inner_text())
        self.assertIn('王*栋',self.page.locator('#alertList').inner_text())
        self.assertEqual(self.page.locator('#modalPatientName').inner_text(),'王*栋')
        self.page.click('#btnAcknowledge')
        self.page.wait_for_function('()=>document.querySelector("#dangerModal").hidden')
        self.view('profile')
        self.page.locator('#settingMask').locator('..').click()
        self.view('summary')
        self.page.wait_for_function('()=>document.querySelector("#patientTableBody").textContent.includes("王国栋")')

    def test_admin_theme_matches_root(self):
        self.admin()
        self.page.evaluate("localStorage.setItem('yihe-theme','dark')")
        self.page.reload()
        self.page.wait_for_function('()=>window.AdminApp && typeof AdminApp.refreshSummary==="function"')
        self.assertTrue(self.page.locator('#settingDarkMode').is_checked())
        self.assertTrue(self.page.evaluate("document.body.classList.contains('dark')"))

    def test_theme_migration_precedence_and_storage_failure(self):
        self.page.add_init_script('''(()=>{
            localStorage.setItem('hm-p-theme',JSON.stringify('light'));
            localStorage.setItem('hm-settings',JSON.stringify({darkMode:true}));
        })()''')
        self.go('home.html')
        self.assertEqual(self.page.evaluate('YiheTheme.get()'),'light')
        self.assertEqual(self.page.evaluate("localStorage.getItem('yihe-theme')"),'light')
        self.page.evaluate("localStorage.setItem('yihe-theme','dark')")
        self.page.reload()
        self.assertEqual(self.page.evaluate('YiheTheme.get()'),'dark')
        self.page.evaluate('''()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==='yihe-theme')throw Error('disk full');return original.call(this,k,v)}}''')
        self.page.on('dialog', lambda d: d.accept())
        self.assertFalse(self.page.evaluate("YiheTheme.set('light')"))
        self.assertEqual(self.page.evaluate('YiheTheme.get()'),'dark')
        self.assertEqual(self.page.locator('#themeToggle').get_attribute('aria-pressed'),'true')

    def test_admin_theme_storage_failure_restores_switch(self):
        self.admin()
        self.view('profile')
        original=self.page.locator('#settingDarkMode').is_checked()
        self.page.evaluate('''()=>{const original=Storage.prototype.setItem;Storage.prototype.setItem=function(k,v){if(k==='yihe-theme')throw Error('disk full');return original.call(this,k,v)}}''')
        self.page.locator('#settingDarkMode').locator('..').click()
        self.assertEqual(self.page.locator('#settingDarkMode').is_checked(),original)
        self.assertEqual(self.page.evaluate("document.body.classList.contains('dark')"),original)

    def test_theme_cross_tab_and_three_entries(self):
        self.go('home.html')
        second=self.context.new_page()
        second.goto(self.url+'home.html')
        self.page.click('#themeToggle')
        expected=self.page.evaluate('YiheTheme.get()')
        second.wait_for_function('expected=>YiheTheme.get()===expected',arg=expected)
        self.assertEqual(second.locator('#themeToggle').get_attribute('aria-pressed'),str(expected=='dark').lower())
        self.admin()
        self.view('profile')
        admin_before=self.page.evaluate('getComputedStyle(document.body).backgroundColor')
        self.page.locator('#settingDarkMode').locator('..').click()
        after_admin=self.page.evaluate('YiheTheme.get()')
        self.page.wait_for_function('before=>getComputedStyle(document.body).backgroundColor!==before',arg=admin_before)
        second.wait_for_function('expected=>YiheTheme.get()===expected',arg=after_admin)
        self.login()
        self.page.click('[data-tab="me"]')
        resident_before=self.page.evaluate('getComputedStyle(document.body).backgroundColor')
        self.page.click('#themeToggleMe')
        after_resident=self.page.evaluate('YiheTheme.get()')
        self.page.wait_for_function('before=>getComputedStyle(document.body).backgroundColor!==before',arg=resident_before)
        second.wait_for_function('expected=>YiheTheme.get()===expected',arg=after_resident)
        self.assertEqual(self.page.locator('#themeToggleMe').get_attribute('aria-pressed'),str(after_resident=='dark').lower())
        self.assertEqual(self.errors,[])

    def test_csv_and_print_preview_mask_snapshot(self):
        self.admin()
        self.view('profile')
        self.page.locator('#settingMask').locator('..').click()
        self.view('sharing')
        self.page.wait_for_function('()=>document.querySelectorAll("#exportResidents input").length===12')
        self.page.click('#btnDeselectAll')
        self.page.locator('#exportResidents input').first.check()
        self.page.evaluate('''()=>{const original=URL.createObjectURL;URL.createObjectURL=function(blob){window.__exportBlob=blob;return original.call(this,blob)}}''')
        with self.page.expect_download():
            self.page.click('#btnExport')
        csv=self.page.evaluate('''async()=>await window.__exportBlob.text()''')
        self.assertIn('王*栋',csv)
        self.assertNotIn('王国栋',csv)
        self.page.evaluate('''()=>{
            window.__originalGetDetail=API.getPatientDetail.bind(API);
            API.getPatientDetail=id=>new Promise(resolve=>{window.__releaseExport=()=>window.__originalGetDetail(id).then(resolve)});
        }''')
        self.page.click('#btnExport')
        self.page.wait_for_function('()=>!!window.__releaseExport')
        self.page.evaluate('AdminApp.settings.mask=false')
        with self.page.expect_download():
            self.page.evaluate('()=>window.__releaseExport()')
        snapshot_csv=self.page.evaluate('''async()=>await window.__exportBlob.text()''')
        self.assertIn('王*栋',snapshot_csv)
        self.assertNotIn('王国栋',snapshot_csv)
        self.page.evaluate('AdminApp.settings.mask=true')
        self.page.evaluate('()=>{API.getPatientDetail=window.__originalGetDetail}')
        self.page.check('input[name="exportFormat"][value="pdf"]')
        self.page.evaluate('''()=>{window.open=()=>({document:{write:s=>window.__pdf=s,close(){}},print(){}})}''')
        self.page.click('#btnExport')
        self.page.wait_for_function('()=>!!window.__pdf')
        pdf=self.page.evaluate('window.__pdf')
        self.assertIn('王*栋',pdf)
        self.assertNotIn('王国栋',pdf)


if __name__ == '__main__':
    unittest.main()
