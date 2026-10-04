"""Privacy contract and critical business regressions for the v0.2.1 baseline."""
import contextlib
import json
import sqlite3
import tempfile
from pathlib import Path
from unittest import mock

from test_v020 import BackendCase, Client
import service
import care


class ContractCase(BackendCase):
    def login(self, role='staff', username='nurse1', password='correct-horse-1'):
        client = Client(self.app)
        self.assertEqual(client.login(role, username, password)[0], 200)
        return client

    def read_rows(self, table):
        with contextlib.closing(self.store.connect()) as db:
            return [dict(row) for row in db.execute('SELECT * FROM ' + table)]

    def add_second_resident(self):
        self.store.add_resident({'id': 2, 'name': '另一居民', 'age': 65, 'responsible': 'staff-2'})
        self.store.add_user('res2', 'resident', '2', '居民二', 'correct-horse-4')

    def add_dose(self, client, offset=60, key='dose'):
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses',
                                    {'name': '合成用药', 'dueAt': service.stamp(self.clock() + offset)},
                                    {'Idempotency-Key': key})[0], 204)
        return client.snapshot()['residents']['1']['doses'][-1]


class PrivacyContract(ContractCase):
    def populated_snapshot(self):
        self.danger()
        staff = self.login()
        self.add_dose(staff)
        for action in ('claim', 'processing'):
            self.assertEqual(self.transition(staff, action), 204)
        self.assertEqual(self.transition(staff, 'escalated', '内部备注-canary', 'staff-2'), 204)
        with self.store.transaction() as db:
            resident = self.store.resident(db, 1)
            resident.update({'internalNotes': 'profile-canary', 'audit': {'actor': 'private-canary'},
                             'contacts': [{'name': '家属', 'phone': '13800138000', 'staff': 'contact-canary'}],
                             'trend': [{'at': service.stamp(self.clock()), 'bloodOxygen': 80, 'actor': 'trend-canary'}]})
            resident['pos']['owner'] = 'position-canary'
            resident['metricStates']['private'] = 'metrics-canary'
            self.store.save(db, 'residents', 1, resident)
            for table in ('care_events', 'medication_tasks'):
                for row in db.execute('SELECT * FROM ' + table).fetchall():
                    value = json.loads(row['data'])
                    value['internalNotes'] = table + '-canary'
                    if table == 'care_events': value['notification']['audit'] = 'notification-canary'
                    else: value['source'] = 'source-canary'
                    self.store.save(db, table, row['id'], value, row['resident_id'])
            row = db.execute('SELECT * FROM event_history ORDER BY id DESC LIMIT 1').fetchone()
            value = json.loads(row['data'])
            value.update({'private': 'history-canary', 'action': 'private-action-canary'})
            db.execute('UPDATE event_history SET data=? WHERE id=?', (service.encode(value), row['id']))
        resident = self.login('resident', 'res1', 'correct-horse-3')
        return staff.snapshot(), resident.snapshot()

    def test_every_resident_object_uses_public_fields(self):
        _, view = self.populated_snapshot()
        self.assertEqual(set(view), {'revision', 'serverTime', 'staff', 'residents', 'events', 'logs'})
        self.assertEqual(view['staff'], [{'id': 'care-team', 'name': '照护团队'}])
        self.assertEqual(view['logs'], [])
        resident = view['residents']['1']
        self.assertEqual(set(resident), {'id', 'name', 'age', 'risk', 'revision', 'deviceState',
                                        'lastSeen', 'updatedAt', 'locationAt', 'bloodOxygen',
                                        'responsible', 'contacts', 'pos', 'metricStates', 'trend', 'doses'})
        self.assertEqual(resident['responsible'], 'care-team')
        self.assertEqual(set(resident['contacts'][0]), {'name', 'phone'})
        self.assertEqual(set(resident['pos']), {'x', 'y'})
        self.assertEqual(set(resident['metricStates']), {'bloodOxygen'})
        self.assertEqual(set(resident['trend'][0]), {'at', 'bloodOxygen'})
        self.assertEqual(set(resident['doses'][0]), {'id', 'name', 'dueAt', 'createdAt', 'source'})
        self.assertEqual(resident['doses'][0]['source'], '工作人员登记')
        event = view['events'][0]
        self.assertEqual(set(event), {'id', 'residentId', 'type', 'severity', 'detail', 'state', 'owner',
                                     'revision', 'createdAt', 'dueAt', 'updatedAt', 'notification', 'history'})
        self.assertEqual(event['owner'], 'care-team')
        self.assertEqual(set(event['notification']), {'cycle', 'deliveredCycle', 'presentedCycle', 'acknowledgedCycle'})
        for item in event['history']:
            self.assertEqual(set(item), {'at', 'actor', 'action', 'note', 'state', 'owner'})
            self.assertIn(item['actor'], ('system', '工作人员'))
            self.assertIn(item['owner'], (None, 'care-team'))
            self.assertEqual(item['note'], '')
        self.assertEqual(event['history'][-1]['action'], '状态更新')

    def test_internal_identities_notes_and_nested_canaries_never_reach_residents(self):
        staff, resident = self.populated_snapshot()
        payload = json.dumps(resident, ensure_ascii=False)
        for secret in ('staff-1', 'staff-2', 'nurse1', 'nurse2', '护士甲', '护士乙', 'canary'):
            self.assertNotIn(secret, payload)
        self.assertEqual(staff['residents']['1']['responsible'], 'staff-1')
        self.assertEqual(staff['events'][0]['owner'], 'staff-2')
        self.assertEqual(staff['residents']['1']['doses'][0]['actor'], 'nurse1')
        self.assertTrue(staff['logs'])
        self.assertIn('内部备注-canary', json.dumps(staff, ensure_ascii=False))

    def test_unassigned_owner_remains_null_and_serializer_does_not_mutate_storage(self):
        self.danger()
        staff = self.login()
        before = staff.snapshot()
        resident = self.login('resident', 'res1', 'correct-horse-3').snapshot()
        self.assertIsNone(resident['events'][0]['owner'])
        self.assertEqual(before, staff.snapshot())

    def test_sensitive_objects_cannot_be_smuggled_into_scalar_public_fields(self):
        import serializers
        resident = self.login('resident', 'res1', 'correct-horse-3').snapshot()['residents']['1']
        resident['heartRate'] = {'actor': 'smuggled-canary'}
        resident['trend'].append({'at': service.stamp(self.clock()), 'temperature': {'staff': 'smuggled-canary'}})
        result = serializers.resident_view(resident)
        self.assertNotIn('smuggled-canary', json.dumps(result))
        resident.pop('metricStates')
        self.assertEqual(serializers.resident_view(resident)['metricStates'], {})


class ResidentAndStaffAccess(ContractCase):
    def test_resident_snapshot_contains_only_own_resident_events_and_doses(self):
        self.add_second_resident()
        self.danger()
        with self.store.transaction() as db:
            self.store.signal(db, 2, 'health', 'other', True, 'danger', '其他居民信号')
            self.store.save(db, 'medication_tasks', 'other-dose',
                            {'id': 'other-dose', 'name': '其他居民用药', 'dueAt': service.stamp(self.clock() + 60)}, 2)
        first = self.login('resident', 'res1', 'correct-horse-3').snapshot()
        second = self.login('resident', 'res2', 'correct-horse-4').snapshot()
        self.assertEqual(set(first['residents']), {'1'})
        self.assertEqual(set(second['residents']), {'2'})
        self.assertEqual({e['residentId'] for e in first['events']}, {1})
        self.assertNotIn('other-dose', json.dumps(first))
        self.assertNotIn('合成危险信号', json.dumps(second, ensure_ascii=False))

    def test_resident_cannot_modify_other_profile_or_responsible_staff(self):
        self.add_second_resident()
        client = self.login('resident', 'res1', 'correct-horse-3')
        for path, body in [('/api/v1/residents/2', {'expectedRevision': 0, 'name': '越权'}),
                           ('/api/v1/residents/1', {'expectedRevision': 0, 'responsible': 'staff-2'})]:
            self.assertEqual(client.call('PATCH', path, body)[0], 403)
        self.assertEqual(client.snapshot()['residents']['1']['revision'], 0)

    def test_resident_cannot_use_staff_actions(self):
        self.danger()
        resident = self.login('resident', 'res1', 'correct-horse-3')
        event = resident.snapshot()['events'][0]
        for path, body in [('/api/v1/audit', None),
                           ('/api/v1/events/presented', {'events': [{'id': event['id'], 'cycle': 1}]}),
                           (f"/api/v1/events/{event['id']}/transitions", {'revision': 1, 'action': 'claim'}),
                           ('/api/v1/residents/1/doses', {'name': '合成', 'dueAt': service.stamp(self.clock())})]:
            self.assertEqual(resident.call('GET' if body is None else 'POST', path, body)[0], 403)

    def test_non_owner_staff_cannot_process_or_close_someone_elses_event(self):
        self.danger()
        first, second = self.login(), self.login('staff', 'nurse2', 'correct-horse-2')
        self.assertEqual(self.transition(first, 'claim'), 204)
        self.assertEqual(self.transition(second, 'processing'), 403)
        self.assertEqual(self.transition(first, 'processing'), 204)
        self.assertEqual(self.transition(second, 'resolved', '越权'), 403)
        self.assertEqual(first.snapshot()['events'][0]['owner'], 'staff-1')

    def test_staff_scope_is_single_community_and_actor_is_session_derived(self):
        self.add_second_resident()
        client = self.login()
        self.assertEqual(set(client.snapshot()['residents']), {'1', '2'})
        self.assertEqual(client.call('PATCH', '/api/v1/residents/2', {'expectedRevision': 0, 'name': '允许修改'})[0], 204)
        self.assertEqual(json.loads(self.read_rows('audit_logs')[-1]['data'])['actor'], 'nurse1')

    def test_unauthenticated_snapshot_and_missing_objects_are_rejected(self):
        self.assertEqual(Client(self.app).call('GET', '/api/v1/care/snapshot')[0], 401)
        client = self.login()
        self.assertEqual(client.call('PATCH', '/api/v1/residents/999', {'expectedRevision': 0})[0], 404)
        self.assertEqual(client.call('POST', '/api/v1/events/missing/transitions', {'revision': 1})[0], 404)


class ResidentUpdates(ContractCase):
    def test_two_concurrent_updates_with_same_revision_allow_only_one_commit(self):
        from concurrent.futures import ThreadPoolExecutor
        import threading
        first, second = self.login(), self.login()
        barrier = threading.Barrier(2)
        def update(client, name):
            barrier.wait(timeout=5)
            return client.call('PATCH', '/api/v1/residents/1', {'expectedRevision': 0, 'name': name})[0]
        with ThreadPoolExecutor(max_workers=2) as pool:
            futures = [pool.submit(update, first, '并发甲'), pool.submit(update, second, '并发乙')]
            self.assertEqual(sorted(future.result(timeout=10) for future in futures), [204, 409])
        self.assertEqual(first.snapshot()['residents']['1']['revision'], 1)
        self.assertEqual(len(self.read_rows('audit_logs')), 1)

    def test_resident_can_update_profile_contacts_and_increment_revision(self):
        client = self.login('resident', 'res1', 'correct-horse-3')
        body = {'expectedRevision': 0, 'name': ' 新姓名 ', 'age': 71,
                'contacts': [{'name': ' 家属 ', 'phone': '+86 13800138000'}]}
        self.assertEqual(client.call('PATCH', '/api/v1/residents/1', body)[0], 204)
        view = client.snapshot()['residents']['1']
        self.assertEqual((view['name'], view['age'], view['revision']), ('新姓名', 71, 1))
        self.assertEqual(view['contacts'][0]['name'], '家属')

    def test_contact_updates_replace_and_append_without_losing_other_contacts(self):
        client = self.login()
        for revision, index, name in [(0, 0, '甲'), (1, 1, '乙'), (2, 0, '丙')]:
            body = {'expectedRevision': revision, 'contactUpdates': [{'index': index, 'value': {'name': name, 'phone': '12345'}}]}
            self.assertEqual(client.call('PATCH', '/api/v1/residents/1', body)[0], 204)
        self.assertEqual([c['name'] for c in client.snapshot()['residents']['1']['contacts']], ['丙', '乙'])

    def test_invalid_updates_are_atomic(self):
        client = self.login()
        bad = [{'actor': 'forged'}, {'age': True}, {'age': 121}, {'name': ''},
               {'responsible': 'unknown'}, {'contacts': [{'name': '甲', 'phone': 'letters'}]},
               {'contacts': [], 'contactUpdates': []}, {'contactUpdates': [{'index': 2, 'value': {}}]},
               {'contacts': [{'name': '甲', 'phone': '12345', 'note': 'internal'}]},
               {'contactUpdates': []}]
        for patch in bad:
            with self.subTest(patch=patch):
                self.assertEqual(client.call('PATCH', '/api/v1/residents/1', {'expectedRevision': 0, **patch})[0], 422)
                self.assertEqual(client.snapshot()['residents']['1']['revision'], 0)
        self.assertEqual(self.read_rows('audit_logs'), [])

    def test_stale_revision_and_staff_assignment(self):
        client = self.login()
        body = {'expectedRevision': 0, 'responsible': 'staff-2'}
        self.assertEqual(client.call('PATCH', '/api/v1/residents/1', body)[0], 204)
        self.assertEqual(client.call('PATCH', '/api/v1/residents/1', body)[0], 409)
        self.assertEqual(client.snapshot()['residents']['1']['responsible'], 'staff-2')


class MedicationAndPresentation(ContractCase):
    def test_dose_creation_confirmation_is_idempotent_and_hides_actor(self):
        staff = self.login()
        dose = self.add_dose(staff)
        resident = self.login('resident', 'res1', 'correct-horse-3')
        self.assertNotIn('actor', resident.snapshot()['residents']['1']['doses'][0])
        path = f"/api/v1/residents/1/doses/{dose['id']}/confirm"
        self.assertEqual(resident.call('POST', path, {})[0], 204)
        revision = resident.snapshot()['revision']
        self.assertEqual(resident.call('POST', path, {})[0], 204)
        self.assertEqual(resident.snapshot()['revision'], revision)
        self.assertIn('confirmedAt', resident.snapshot()['residents']['1']['doses'][0])

    def test_confirmation_cannot_be_early_or_cross_resident(self):
        self.add_second_resident()
        staff = self.login()
        dose = self.add_dose(staff, offset=3600)
        own = self.login('resident', 'res1', 'correct-horse-3')
        other = self.login('resident', 'res2', 'correct-horse-4')
        path = f"/api/v1/residents/1/doses/{dose['id']}/confirm"
        self.assertEqual(own.call('POST', path, {})[0], 422)
        self.assertEqual(other.call('POST', path, {})[0], 403)
        self.assertEqual(own.call('POST', '/api/v1/residents/1/doses/missing/confirm', {})[0], 404)
        self.assertEqual(own.call('POST', path, {'actor': 'forged'})[0], 422)
        self.clock.advance(1800)
        self.assertEqual(own.call('POST', path, {})[0], 204)

    def test_overdue_boundary_worker_recovery_and_manual_resolution(self):
        staff = self.login()
        dose = self.add_dose(staff)
        self.clock.advance(60 + 1800)
        self.store.tick()
        self.assertFalse(any(e['type'] == 'medication' for e in staff.snapshot()['events']))
        self.clock.advance(1)
        self.store.tick()
        event = next(e for e in staff.snapshot()['events'] if e['type'] == 'medication')
        resident = self.login('resident', 'res1', 'correct-horse-3')
        self.assertEqual(resident.call('POST', f"/api/v1/residents/1/doses/{dose['id']}/confirm", {})[0], 204)
        recovered = next(e for e in staff.snapshot()['events'] if e['id'] == event['id'])
        self.assertIn('sourceRecoveredAt', recovered)
        self.assertEqual(recovered['state'], 'new')

    def test_presented_receipt_does_not_acknowledge_and_repeat_is_noop(self):
        self.danger()
        client = self.login()
        event = client.snapshot()['events'][0]
        body = {'events': [{'id': event['id'], 'cycle': 1}]}
        self.assertEqual(client.call('POST', '/api/v1/events/presented', body)[0], 204)
        view = client.snapshot()
        self.assertEqual(view['events'][0]['notification'], {'cycle': 1, 'deliveredCycle': 1, 'presentedCycle': 1, 'acknowledgedCycle': 0})
        self.assertEqual(client.call('POST', '/api/v1/events/presented', body)[0], 204)
        self.assertEqual(client.snapshot()['revision'], view['revision'])

    def test_stale_or_missing_receipt_rolls_back_whole_batch(self):
        self.danger()
        client = self.login()
        event = client.snapshot()['events'][0]
        for item, status in [({'id': event['id'], 'cycle': 2}, 409), ({'id': 'missing', 'cycle': 1}, 404)]:
            body = {'events': [{'id': event['id'], 'cycle': 1}, item]}
            self.assertEqual(client.call('POST', '/api/v1/events/presented', body)[0], status)
            self.assertEqual(client.snapshot()['events'][0]['notification']['presentedCycle'], 0)
        self.assertEqual(client.call('POST', '/api/v1/events/presented', {'events': [{'id': event['id'], 'cycle': 0}]})[0], 422)

    def test_medication_creation_validation_and_limit(self):
        client = self.login()
        for body in ({'name': '', 'dueAt': service.stamp(self.clock())},
                     {'name': '合成', 'dueAt': service.stamp(self.clock() - 61)},
                     {'name': '合成', 'dueAt': '2026-01-01'},
                     {'name': '合成', 'dueAt': service.stamp(self.clock()), 'actor': 'forged'}):
            self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', body, {'Idempotency-Key': 'bad'})[0], 422)
        with self.store.transaction() as db:
            for index in range(500):
                self.store.save(db, 'medication_tasks', str(index),
                                {'id': str(index), 'name': '合成', 'dueAt': service.stamp(self.clock() + 60)}, 1)
        status, value = client.call('POST', '/api/v1/residents/1/doses',
                                    {'name': '合成', 'dueAt': service.stamp(self.clock())}, {'Idempotency-Key': 'limit'})
        self.assertEqual((status, value['error']['code']), (422, 'LIMIT_EXCEEDED'))


class SignalsAndTransitions(ContractCase):
    def test_signal_recovery_recurrence_and_worsening_create_notification_cycles(self):
        with self.store.transaction() as db:
            self.store.signal(db, 1, 'health', 'sim', False, 'warning', '恢复')
            self.store.signal(db, 1, 'health', 'sim', True, 'warning', '初始')
        staff = self.login()
        initial = staff.snapshot()['events'][0]
        with self.store.transaction() as db:
            self.store.signal(db, 1, 'health', 'sim', False, 'warning', '恢复')
        self.assertIn('sourceRecoveredAt', staff.snapshot()['events'][0])
        with self.store.transaction() as db:
            self.store.signal(db, 1, 'health', 'sim', True, 'danger', '再发')
        recurrent = staff.snapshot()['events'][0]
        self.assertEqual(recurrent['id'], initial['id'])
        self.assertEqual(recurrent['notification']['cycle'], 2)
        self.assertEqual(recurrent['severity'], 'danger')
        self.assertNotIn('sourceRecoveredAt', recurrent)
        self.assertLess(service.epoch(recurrent['dueAt']), service.epoch(initial['dueAt']))
        with self.store.transaction() as db:
            self.store.signal(db, 1, 'health', 'sim', True, 'danger', '再发')
        self.assertEqual(staff.snapshot()['events'][0]['revision'], recurrent['revision'])

    def test_active_signal_reopens_after_resolution_and_false_alarm(self):
        for outcome in ('resolved', 'false_alarm'):
            with self.subTest(outcome=outcome):
                self.danger(key=outcome)
                client = self.login()
                previous = client.snapshot()['events'][0]
                for action in ('claim', 'processing'):
                    self.assertEqual(self.transition(client, action), 204)
                self.assertEqual(self.transition(client, outcome, '已人工复核'), 204)
                view = client.snapshot()
                closed = next(e for e in view['events'] if e['id'] == previous['id'])
                new = next(e for e in view['events'] if e['state'] == 'new')
                self.assertEqual(closed['state'], outcome)
                self.assertIn('closedAt', closed)
                self.assertEqual(new['previousIncidentId'], previous['id'])

    def test_recovered_signal_closes_without_reopening(self):
        self.danger()
        client = self.login()
        with self.store.transaction() as db:
            self.store.signal(db, 1, 'health', 'sim', False, 'danger', '恢复')
        for action in ('claim', 'processing'):
            self.assertEqual(self.transition(client, action), 204)
        self.assertEqual(self.transition(client, 'resolved', '核实完成'), 204)
        self.assertEqual([e['state'] for e in client.snapshot()['events']], ['resolved'])

    def test_revision_conflict_invalid_state_and_required_notes(self):
        self.danger()
        client = self.login()
        event = client.snapshot()['events'][0]
        path = f"/api/v1/events/{event['id']}/transitions"
        self.assertEqual(client.call('POST', path, {'revision': 0, 'action': 'claim'})[0], 409)
        self.assertEqual(client.call('POST', path, {'revision': 1, 'action': 'claim'})[0], 204)
        self.assertEqual(client.call('POST', path, {'revision': 1, 'action': 'claim'})[0], 409)
        self.assertEqual(self.transition(client, 'resolved', '跳过处理中'), 409)
        self.assertEqual(self.transition(client, 'processing'), 204)
        self.assertEqual(self.transition(client, 'resolved', ' '), 422)
        self.assertEqual(self.transition(client, 'escalated', '交接', 'staff-1'), 422)
        self.assertEqual(self.transition(client, 'escalated', '交接', 'missing'), 422)
        self.assertEqual(self.transition(client, 'resolved', 'x' * 2001), 422)

    def test_tick_detects_offline_and_heartbeat_recovers_without_closing_incident(self):
        client = self.login()
        self.clock.advance(300)
        self.store.tick()
        self.assertEqual(client.snapshot()['events'], [])
        self.clock.advance(1)
        self.store.tick()
        event = client.snapshot()['events'][0]
        self.assertEqual(event['type'], 'offline')
        self.assertEqual(client.snapshot()['residents']['1']['deviceState'], 'offline')
        device = Client(self.app)
        self.assertEqual(device.call('POST', '/api/v1/device/events', {'residentId': 1, 'type': 'heartbeat'},
                                     {'Authorization': 'Bearer devkey', 'Idempotency-Key': 'heartbeat'})[0], 204)
        view = client.snapshot()
        self.assertEqual(view['residents']['1']['deviceState'], 'online')
        self.assertEqual(view['events'][0]['id'], event['id'])
        self.assertIn('sourceRecoveredAt', view['events'][0])
        outbox = self.read_rows('notification_outbox')
        self.assertEqual({(row['channel'], row['status']) for row in outbox}, {('page', 'available'), ('sms', 'disabled')})


class InquiriesAuditAndIdempotency(ContractCase):
    def test_anonymous_inquiry_is_idempotent_and_changed_payload_conflicts(self):
        client = Client(self.app)
        client.call('GET', '/api/v1/session')
        body = {'name': '合成人', 'phone': '12345', 'type': '咨询', 'message': '合成咨询'}
        headers = {'Idempotency-Key': 'inquiry'}
        self.assertEqual(client.call('POST', '/api/v1/inquiries', body, headers)[0], 204)
        self.assertEqual(client.call('POST', '/api/v1/inquiries', body, headers)[0], 204)
        self.assertEqual(len(self.read_rows('inquiries')), 1)
        self.assertEqual(client.call('POST', '/api/v1/inquiries', {**body, 'message': '不同内容'}, headers)[0], 409)
        self.assertEqual(client.call('POST', '/api/v1/inquiries', {**body, 'message': ''}, {'Idempotency-Key': 'new'})[0], 422)

    def test_device_and_dose_idempotency_survive_new_clients(self):
        self.assertEqual(self.danger('device-key'), 204)
        self.assertEqual(self.danger('device-key'), 204)
        self.assertEqual(len(self.read_rows('audit_logs')), 1)
        client = self.login()
        body = {'name': '合成', 'dueAt': service.stamp(self.clock())}
        headers = {'Idempotency-Key': 'same-key'}
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', body, headers)[0], 204)
        another = self.login()
        self.assertEqual(another.call('POST', '/api/v1/residents/1/doses', body, headers)[0], 204)
        self.assertEqual(len(self.read_rows('medication_tasks')), 1)
        self.assertEqual(another.call('POST', '/api/v1/residents/1/doses', {**body, 'name': '变更'}, headers)[0], 409)

    def test_idempotency_key_required_and_failed_request_does_not_consume_key(self):
        client = self.login()
        invalid = {'name': '合成', 'dueAt': service.stamp(self.clock() - 100)}
        valid = {**invalid, 'dueAt': service.stamp(self.clock())}
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', valid)[0], 422)
        headers = {'Idempotency-Key': 'retry'}
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', invalid, headers)[0], 422)
        self.assertEqual(client.call('POST', '/api/v1/residents/1/doses', valid, headers)[0], 204)

    def test_audit_cursor_pagination_and_permissions(self):
        client = self.login()
        for revision in range(3):
            self.assertEqual(client.call('PATCH', '/api/v1/residents/1', {'expectedRevision': revision, 'age': 70 + revision})[0], 204)
        from http.cookies import SimpleCookie
        cookie = SimpleCookie(); cookie['yihe_session'] = client.cookie
        with self.store.transaction() as db:
            env = {'REQUEST_METHOD': 'GET', 'PATH_INFO': '/api/v1/audit', 'QUERY_STRING': 'after=0&limit=2'}
            status, first, _ = self.app.route(env, {}, cookie, db)
            self.assertEqual(status, 200)
            self.assertEqual(len(first['items']), 2)
            env['QUERY_STRING'] = f"after={first['next']}&limit=2"
            second = self.app.route(env, {}, cookie, db)[1]
            self.assertEqual(len(second['items']), 1)
            env['QUERY_STRING'] = f"after={second['next']}"
            self.assertEqual(self.app.route(env, {}, cookie, db)[1]['items'], [])
            for query in ('after=x', 'after=-1', 'limit=201'):
                env['QUERY_STRING'] = query
                with self.assertRaises(service.Problem): self.app.route(env, {}, cookie, db)


class SessionAndSecurity(ContractCase):
    def test_login_rotates_session_logout_destroys_it_and_cookie_flags(self):
        client = Client(self.app)
        client.call('GET', '/api/v1/session')
        anonymous, csrf = client.cookie, client.csrf
        self.assertEqual(client.call('POST', '/api/v1/session', {'role': 'staff', 'username': 'nurse1', 'password': 'correct-horse-1'})[0], 200)
        authenticated = client.cookie
        self.assertNotEqual(anonymous, authenticated)
        self.assertNotEqual(csrf, client.csrf)
        stale = Client(self.app); stale.cookie = anonymous
        self.assertEqual(stale.call('GET', '/api/v1/care/snapshot')[0], 401)
        self.assertEqual(client.call('DELETE', '/api/v1/session')[0], 204)
        stale.cookie = authenticated
        self.assertEqual(stale.call('GET', '/api/v1/care/snapshot')[0], 401)
        self.assertEqual(self.read_rows('sessions'), [])

    def test_secure_session_cookie_and_no_plaintext_token_in_database(self):
        app = service.CareApplication(self.store, 'https://care.example')
        client = Client(app)
        captured = {}
        def start(status, headers): captured.update(status=status, headers=dict(headers))
        import io
        app({'REQUEST_METHOD': 'GET', 'PATH_INFO': '/api/v1/session', 'wsgi.input': io.BytesIO()}, start)
        cookie = captured['headers']['Set-Cookie']
        for flag in ('HttpOnly', 'SameSite=Lax', 'Secure', 'Path=/'): self.assertIn(flag, cookie)
        token = cookie.split('=', 1)[1].split(';')[0]
        self.assertNotIn(token, json.dumps(self.read_rows('sessions')))

    def test_session_expiry_and_creation_cleans_expired_sessions(self):
        client = self.login('resident', 'res1', 'correct-horse-3')
        self.clock.advance(86401)
        self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 401)
        self.assertIsNone(client.call('GET', '/api/v1/session')[1]['user'])
        self.assertEqual(len(self.read_rows('sessions')), 1)

    def test_csrf_and_origin_rejection_leave_database_unchanged(self):
        client = self.login()
        for headers, code in [({'Origin': 'https://evil.example'}, 'ORIGIN_REJECTED'),
                              ({'X-CSRF-Token': 'wrong'}, 'CSRF_REJECTED')]:
            status, value = client.call('PATCH', '/api/v1/residents/1', {'expectedRevision': 0, 'name': '变更'}, headers)
            self.assertEqual((status, value['error']['code']), (403, code))
        self.assertEqual(client.snapshot()['residents']['1']['revision'], 0)
        self.assertEqual(self.read_rows('audit_logs'), [])
        self.assertEqual(Client(self.app).call('POST', '/api/v1/inquiries', {})[0], 401)

    def test_device_authentication_validation_and_bounded_trend(self):
        device = Client(self.app)
        body = {'residentId': 1, 'type': 'heartbeat'}
        self.assertEqual(device.call('POST', '/api/v1/device/events', body)[0], 401)
        disabled = Client(service.CareApplication(self.store, 'http://127.0.0.1:8000', secure=False))
        self.assertEqual(disabled.call('POST', '/api/v1/device/events', body)[0], 503)
        for patch in ({'residentId': True}, {'type': 'unknown'}, {'active': 1}, {'severity': 'unknown'},
                      {'metrics': {'name': 'forged'}}, {'metrics': {'heartRate': True}},
                      {'metricStates': {'heartRate': 'invalid'}}):
            self.assertEqual(device.call('POST', '/api/v1/device/events', {**body, **patch},
                                        {'Authorization': 'Bearer devkey', 'Idempotency-Key': 'bad'})[0], 422)
        for index in range(74):
            self.assertEqual(device.call('POST', '/api/v1/device/events', {**body, 'metrics': {'heartRate': index}},
                                        {'Authorization': 'Bearer devkey', 'Idempotency-Key': str(index)})[0], 204)
        view = self.login().snapshot()['residents']['1']
        self.assertEqual(len(view['trend']), 72)
        self.assertEqual(view['trend'][0]['heartRate'], 2)

    def test_malformed_json_nan_and_payload_limit(self):
        import io
        for raw, length, expected in [(b'[]', 2, 400), (b'{bad', 4, 400),
                                       (b'{"x":NaN}', 9, 400), (b'', 65537, 413), (b'', -1, 413)]:
            result = {}
            def start(status, _): result['status'] = int(status.split()[0])
            self.app({'REQUEST_METHOD': 'POST', 'PATH_INFO': '/api/v1/session',
                      'CONTENT_LENGTH': str(length), 'wsgi.input': io.BytesIO(raw)}, start)
            self.assertEqual(result['status'], expected)

    def test_static_path_traversal_and_external_symlinks_are_rejected(self):
        import io
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'public'; root.mkdir()
            (root / 'home.html').write_text('public')
            private = Path(directory) / 'private.txt'; private.write_text('private-canary')
            (root / 'outside.txt').symlink_to(private)
            app = service.CareApplication(self.store, 'http://127.0.0.1:8000', root, secure=False)
            for path in ('/../private.txt', '/%2e%2e/private.txt', '/outside.txt', '/missing'):
                result = {}
                def start(status, _): result['status'] = int(status.split()[0])
                response = b''.join(app({'REQUEST_METHOD': 'GET', 'PATH_INFO': path, 'wsgi.input': io.BytesIO()}, start))
                self.assertEqual(result['status'], 404)
                self.assertNotIn(b'private-canary', response)
            for method in ('GET', 'HEAD'):
                result = {}
                def start(status, headers): result.update(status=int(status.split()[0]), headers=dict(headers))
                response = b''.join(app({'REQUEST_METHOD': method, 'PATH_INFO': '/', 'wsgi.input': io.BytesIO()}, start))
                self.assertEqual(result['status'], 200)
                self.assertEqual(response, b'public' if method == 'GET' else b'')
                self.assertEqual(result['headers']['Content-Length'], '6')


class ConnectionLifecycle(ContractCase):
    def test_initialization_closes_connection_including_schema_failure(self):
        opened = []
        connect = sqlite3.connect
        class TrackedConnection(sqlite3.Connection):
            closed = False
            def close(self):
                self.closed = True
                super().close()
        def tracked(*args, **kwargs):
            kwargs['factory'] = TrackedConnection
            connection = connect(*args, **kwargs); opened.append(connection)
            return connection
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(service.sqlite3, 'connect', tracked):
            service.CareStore(Path(directory) / 'new.sqlite3')
            with mock.patch.object(care, 'SCHEMA', 'INVALID SQL'):
                with self.assertRaises(sqlite3.Error): service.CareStore(Path(directory) / 'failed.sqlite3')
        self.assertEqual(len(opened), 2)
        self.assertTrue(all(connection.closed for connection in opened))

    def test_transaction_commits_rolls_back_and_closes_after_base_exception(self):
        connections = []
        for exception in (None, ValueError, KeyboardInterrupt):
            try:
                with self.store.transaction() as db:
                    connections.append(db)
                    db.execute('INSERT INTO inquiries(data) VALUES(?)', ('{"message":"transaction"}',))
                    if exception: raise exception()
            except (ValueError, KeyboardInterrupt): pass
        self.assertEqual(len(self.read_rows('inquiries')), 1)
        for connection in connections:
            with self.assertRaises(sqlite3.ProgrammingError): connection.execute('SELECT 1')

    def test_connection_closes_when_begin_fails(self):
        connection = mock.Mock()
        connection.execute.side_effect = sqlite3.OperationalError('busy')
        with mock.patch.object(self.store, 'connect', return_value=connection):
            with self.assertRaises(sqlite3.OperationalError):
                with self.store.transaction(): self.fail('transaction must not yield')
        connection.close.assert_called_once()
        connection.rollback.assert_called_once()
