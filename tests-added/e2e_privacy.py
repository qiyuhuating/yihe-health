"""Real HTTP + SQLite + unchanged HTTP adapter: resident privacy and usability."""
import contextlib
import json
import os
import socket
import subprocess
import sys
import tempfile
import time
import urllib.request
from pathlib import Path

from playwright.sync_api import sync_playwright
from browser_support import launch_chromium

ROOT = Path(__file__).resolve().parents[1]
SEED = '''
import sys
sys.path.insert(0, sys.argv[2])
import service
store = service.CareStore(sys.argv[1])
store.add_user('nurse1', 'staff', 'staff-1', '护士甲', 'correct-horse-1')
store.add_user('nurse2', 'staff', 'staff-2', '护士乙', 'correct-horse-2')
store.add_resident({'id': 1, 'name': '合成居民', 'age': 70, 'responsible': 'staff-1',
                    'contacts': [{'name': '家属', 'phone': '13800138000'}]})
store.add_resident({'id': 2, 'name': '隔离居民-canary', 'age': 65, 'responsible': 'staff-2'})
store.add_user('res1', 'resident', '1', '合成居民', 'correct-horse-3')
user = {'role': 'staff', 'username': 'nurse1', 'staffId': 'staff-1'}
with store.transaction() as db:
    store.signal(db, 1, 'health', 'private-source-canary', True, 'danger', '合成预警，请人工核实')
    event = store.snapshot(db, user)['events'][0]
    store.transition(db, event['id'], {'action': 'claim', 'revision': event['revision']}, user)
    store.transition(db, event['id'], {'action': 'processing', 'revision': event['revision'] + 1,
                                      'note': '内部处置备注-canary'}, user)
    store.doses(db, 1, {'name': '合成用药', 'dueAt': service.stamp(store.clock())}, user, 'seed-dose')
'''


def main():
    with tempfile.TemporaryDirectory(prefix='yihe-privacy-') as directory:
        db = Path(directory) / 'care.sqlite3'
        subprocess.run([sys.executable, '-c', SEED, str(db), str(ROOT / 'backend')], check=True)
        with contextlib.closing(socket.socket()) as probe:
            probe.bind(('127.0.0.1', 0))
            port = probe.getsockname()[1]
        base = f'http://127.0.0.1:{port}'
        proc = subprocess.Popen([sys.executable, str(ROOT / 'backend/service.py'), '--db', str(db),
                                 'serve', '--port', str(port), '--origin', base, '--insecure-local',
                                 '--static', str(ROOT / 'docs/dist')], stdout=subprocess.DEVNULL)
        try:
            for attempt in range(80):
                if proc.poll() is not None: raise RuntimeError('Backend exited before startup')
                try:
                    with urllib.request.urlopen(base + '/login.html', timeout=1): break
                except OSError: time.sleep(0.1)
            else: raise RuntimeError('Backend did not start')
            with sync_playwright() as playwright:
                browser = launch_chromium(playwright)
                context = browser.new_context(viewport={'width': 390, 'height': 844})
                page = context.new_page()
                errors = []
                page.on('pageerror', lambda error: errors.append(str(error)))
                page.goto(base + '/login.html')
                page.fill('#loginName', 'res1')
                page.fill('#loginPassword', 'correct-horse-3')
                page.click('#loginSubmit')
                page.wait_for_url('**/index.html')
                page.locator('#residentVitals .vital-card').first.wait_for(state='attached')
                view = page.evaluate('async () => await CareAPI.snapshot()')
                assert set(view['residents']) == {'1'}
                assert view['staff'] == [{'id': 'care-team', 'name': '照护团队'}]
                assert view['residents']['1']['responsible'] == 'care-team'
                assert view['events'][0]['owner'] == 'care-team'
                assert view['logs'] == []
                payload = json.dumps(view, ensure_ascii=False)
                for secret in ('staff-1', 'staff-2', 'nurse1', 'nurse2', '护士甲', '护士乙', 'canary'):
                    assert secret not in payload, secret
                page.click('[data-tab=health]')
                assert '照护团队' in page.locator('#residentAlerts').inner_text()
                page.click('[data-tab=me]')
                page.fill('#profileName', '修改后的居民')
                page.locator('#residentProfileForm button[type=submit]').click()
                page.wait_for_function("async () => (await CareAPI.snapshot()).residents['1'].name === '修改后的居民'")
                dose = view['residents']['1']['doses'][0]
                status = page.evaluate("async id => { await CareAPI.confirmDose(1, id); return (await CareAPI.snapshot()).residents['1'].doses[0].confirmedAt; }", dose['id'])
                assert status
                assert not errors, errors
                print('PASS: real backend resident privacy, unchanged adapter, care-team rendering, profile update and dose confirmation')
                context.close()
                browser.close()
        finally:
            proc.terminate()
            try: proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill(); proc.wait(timeout=5)


if __name__ == '__main__': main()
