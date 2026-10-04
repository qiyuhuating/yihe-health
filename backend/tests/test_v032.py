"""v0.3.2 architecture contracts: reads are pure and transaction ownership is explicit."""
import contextlib
import sqlite3
import threading

from test_v022 import ContractCase
import service


class ReadWriteTransactions(ContractCase):
    def test_read_transaction_is_sqlite_enforced_read_only(self):
        with contextlib.closing(self.store.connect()) as db:
            expected = db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()[0]
        with self.store.read_transaction() as db:
            self.assertEqual(db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()[0], expected)
            with self.assertRaises(sqlite3.OperationalError):
                db.execute("UPDATE settings SET value=value+1 WHERE key='revision'")
        with contextlib.closing(self.store.connect()) as db:
            self.assertEqual(db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()[0], expected)

    def test_read_transaction_does_not_block_independent_writer_in_wal_mode(self):
        completed = threading.Event()
        errors = []

        def writer():
            try:
                with self.store.write_transaction() as db:
                    db.execute("UPDATE settings SET value=value+1 WHERE key='revision'")
            except BaseException as exc:
                errors.append(exc)
            finally:
                completed.set()

        with self.store.read_transaction() as db:
            db.execute("SELECT value FROM settings WHERE key='revision'").fetchone()
            thread = threading.Thread(target=writer)
            thread.start()
            self.assertTrue(completed.wait(2), 'read transaction unexpectedly blocked WAL writer')
            thread.join(timeout=1)
        self.assertEqual(errors, [])

    def test_snapshot_is_pure_even_when_time_driven_state_is_due(self):
        client = self.login()
        before_revision = client.snapshot()['revision']
        self.clock.advance(301)
        first = client.snapshot()
        second = client.snapshot()
        self.assertEqual(first['revision'], before_revision)
        self.assertEqual(second['revision'], before_revision)
        self.assertEqual(first['events'], second['events'])
        self.assertEqual(first['residents']['1']['deviceState'], 'unknown')
        self.store.tick()
        after = client.snapshot()
        self.assertGreater(after['revision'], before_revision)
        self.assertEqual(after['residents']['1']['deviceState'], 'offline')
        self.assertTrue(any(event['type'] == 'offline' for event in after['events']))

    def test_snapshot_http_path_does_not_open_write_transaction(self):
        client = self.login()
        original = self.store.write_transaction
        def forbidden():
            raise AssertionError('snapshot must not use a write transaction')
        self.store.write_transaction = forbidden
        try:
            self.assertEqual(client.call('GET', '/api/v1/care/snapshot')[0], 200)
        finally:
            self.store.write_transaction = original

    def test_failed_login_persists_limiter_without_manual_commit_in_login(self):
        # Use a fresh client so the limiter subject is isolated.
        from test_v020 import Client
        client = Client(self.app, addr='203.0.113.7')
        for index in range(5):
            status, payload = client.login('staff', 'nurse1', f'wrong-password-{index}')
            self.assertEqual(status, 401)
            self.assertEqual(payload['error']['code'], 'UNAUTHENTICATED')
        self.assertEqual(client.login('staff', 'nurse1', 'correct-horse-1')[0], 429)
        with contextlib.closing(self.store.connect()) as db:
            rows = db.execute('SELECT failures FROM login_limits').fetchall()
        self.assertEqual([row['failures'] for row in rows], [5])
