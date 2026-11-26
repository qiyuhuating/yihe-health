import importlib.util
from pathlib import Path
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

spec=importlib.util.spec_from_file_location("care_watchdog",Path(__file__).with_name("watchdog.py"))
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)


class WatchdogTests(unittest.TestCase):
    def test_failure_survives_restart_and_resolved_does_not_overtake_firing(self):
        received=[]
        broken=[True]
        class Receiver(BaseHTTPRequestHandler):
            def log_message(self,*args):pass
            def do_POST(self):
                body=self.rfile.read(int(self.headers["Content-Length"]))
                if broken[0]:
                    self.send_response(503)
                else:
                    received.append(body);self.send_response(200)
                self.end_headers()
        server=ThreadingHTTPServer(("127.0.0.1",0),Receiver)
        thread=threading.Thread(target=server.serve_forever,daemon=True);thread.start()
        try:
            with tempfile.TemporaryDirectory() as directory:
                state=Path(directory)/"watchdog.sqlite"
                now=[1000.0]
                args=(state,"http://127.0.0.1:"+str(server.server_port)+"/alerts","synthetic-token-at-least-24-characters")
                first=module.Watchdog(*args,clock=lambda:now[0])
                first.enqueue("Prometheus","2026-10-26T00:00:00Z","firing","2026-10-26T00:00:01Z")
                first.db.commit();first.deliver();first.db.close()
                second=module.Watchdog(*args,clock=lambda:now[0])
                second.enqueue("Prometheus","2026-10-26T00:00:00Z","resolved","2026-10-26T00:00:02Z")
                second.db.commit();broken[0]=False
                second.deliver()
                self.assertEqual(received,[])
                now[0]+=301;second.deliver()
                self.assertEqual(len(received),2)
                self.assertIn(b'"status": "firing"',received[0])
                self.assertIn(b'"status": "resolved"',received[1])
                self.assertEqual(second.db.execute("SELECT count(*) FROM notification WHERE status='pending'").fetchone()[0],0)
                second.db.close()
        finally:
            server.shutdown();server.server_close();thread.join()

    def test_receiver_boundary(self):
        for url in ("http://example.com/alerts","https://user:password@example.com/alerts","ftp://127.0.0.1/alerts"):
            with self.subTest(url=url),self.assertRaises(ValueError):module.validate_receiver(url)
        module.validate_receiver("http://127.0.0.1/alerts")
        module.validate_receiver("https://example.com/alerts")


if __name__=="__main__":unittest.main()
