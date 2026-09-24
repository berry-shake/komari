import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest

ROOT = Path(__file__).resolve().parents[1]
INSTALLER = ROOT / ('install-komari.sh' if ROOT.name == 'komari' else 'install.sh')
BINARY = b'verified binary fixture'


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        if self.path.startswith('/missing'):
            self.send_error(404)
            return
        payload = BINARY
        if self.path.endswith('.sha256'):
            payload = (hashlib.sha256(BINARY).hexdigest() + '\n').encode()
            if self.path.startswith('/corrupt'):
                payload = b'0' * 64
            if self.path.startswith('/invalid'):
                payload = b'Not Found'
        self.send_response(200)
        self.end_headers()
        self.wfile.write(payload)


class InstallerTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        cls.thread = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.thread.start()
        cls.url = f'http://127.0.0.1:{cls.server.server_port}'

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.thread.join()

    def test_success_stages_a_verified_executable_without_overwriting(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / 'agent'
            target.write_bytes(b'previous version')
            result = subprocess.run(['bash', '-c', 'source "$1"; if staged=$(download_verified "$2" "$3"); then echo "$staged"; else exit 1; fi', '_', str(INSTALLER), self.url + '/good', str(target)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            staged = Path(result.stdout.strip())
            self.assertEqual(staged.read_bytes(), BINARY)
            self.assertTrue(os.access(staged, os.X_OK))
            self.assertEqual(target.read_bytes(), b'previous version')

    def test_http_errors_and_bad_checksums_preserve_old_binary(self):
        for endpoint in ['/missing', '/corrupt', '/invalid']:
            with self.subTest(endpoint=endpoint), tempfile.TemporaryDirectory() as temp:
                target = Path(temp) / 'agent'
                target.write_bytes(b'previous version')
                result = subprocess.run(['bash', '-c', 'source "$1"; if staged=$(download_verified "$2" "$3"); then echo "$staged"; else exit 1; fi', '_', str(INSTALLER), self.url + endpoint, str(target)], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(target.read_bytes(), b'previous version')
                self.assertEqual(list(Path(temp).iterdir()), [target])

    def test_failed_server_start_rolls_back_exact_backup(self):
        with tempfile.TemporaryDirectory() as temp:
            target = Path(temp) / 'komari'
            target.write_bytes(b'previous version')
            staged = Path(temp) / 'staged'
            staged.write_bytes(b'new version')
            script = r'''source "$1"
BINARY_PATH="$2"
staged_fixture="$3"
is_installed() { return 0; }
check_systemd() { return 0; }
select_channel() { :; }
detect_arch() { echo amd64; }
download_verified() { echo "$staged_fixture"; }
ui_msgbox() { :; }
sleep() { :; }
systemctl() {
    if [[ "$1" == start ]] && [[ "$(cat "$BINARY_PATH")" == 'new version' ]]; then
        return 1
    fi
    return 0
}
upgrade_komari
'''
            result = subprocess.run(['bash', '-c', script, '_', str(INSTALLER), str(target), str(staged)], capture_output=True, text=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(target.read_bytes(), b'previous version')
            self.assertEqual(len(list(Path(temp).glob('komari.backup.*'))), 1)
