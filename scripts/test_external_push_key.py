import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


class ExternalPushKeyProvisioningTest(unittest.TestCase):
    def test_unsafe_terminal_input_is_rejected_on_either_prompt(self):
        # Simulate the stdlib's terminal boundary: getpass warns before falling
        # back to echoed input when disabling terminal echo fails.
        script = Path(__file__).with_name('external-push-key.py')
        for prompt in (1, 2):
            with self.subTest(prompt=prompt), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'test-only.key'
                wrapper = '''
import getpass, runpy, sys, warnings
count = 0
script, warning_prompt, path = sys.argv[1:]
def terminal_input(*args):
    global count
    count += 1
    if count == int(warning_prompt):
        warnings.warn('terminal cannot disable echo', getpass.GetPassWarning)
    return 'TEST_ONLY_NO_ECHO_KEY'
getpass.getpass = terminal_input
sys.argv = [script, '--path', path]
runpy.run_path(script, run_name='__main__')
'''
                result = subprocess.run([sys.executable, '-c', wrapper, str(script), str(prompt), str(path)], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(path.exists())
                self.assertNotIn(b'TEST_ONLY_NO_ECHO_KEY', result.stdout + result.stderr)

    def test_exact_bytes_no_echo_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private' / 'test-only.key'
            command = [sys.executable, str(Path(__file__).with_name('external-push-key.py')), '--stdin', '--path', str(path)]
            key = b' TEST_ONLY_DEPLOYMENT_KEY '
            result = subprocess.run(command, input=key, capture_output=True)
            self.assertEqual(result.returncode, 0)
            self.assertEqual(path.read_bytes(), key)
            if os.name == 'posix':
                self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertNotIn(key.strip(), result.stdout + result.stderr)
            retry = subprocess.run(command, input=b'TEST_REPLACEMENT', capture_output=True)
            self.assertNotEqual(retry.returncode, 0)
            self.assertEqual(path.read_bytes(), key)

    def test_newline_is_rejected_before_creating_file(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'test-only.key'
            result = subprocess.run([sys.executable, str(Path(__file__).with_name('external-push-key.py')), '--stdin', '--path', str(path)], input=b'TEST_ONLY_KEY\n', capture_output=True)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(path.exists())
            self.assertNotIn(b'TEST_ONLY_KEY', result.stdout + result.stderr)

    def test_checkout_path_is_rejected(self):
        checkout = Path(__file__).resolve().parents[1]
        if not (checkout / '.git').exists():
            self.skipTest('requires a Git checkout boundary')
        path = checkout / 'secrets' / 'platform10-test.key'
        result = subprocess.run([sys.executable, str(Path(__file__).with_name('external-push-key.py')), '--stdin', '--path', str(path)], input=b'TEST_ONLY_KEY', capture_output=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertFalse(path.exists())
        self.assertNotIn(b'TEST_ONLY_KEY', result.stdout + result.stderr)
