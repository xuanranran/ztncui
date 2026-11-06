"""Test the installer bootstrap without installing packages or changing the host."""
import pathlib
import os
import re
import tempfile
import unittest
from unittest.mock import Mock, patch


SCRIPT = pathlib.Path(__file__).resolve().parents[1] / 'install.sh'
blocks = re.findall(r"python3[^\n]*<<'PY'\n(.*?)^PY$", SCRIPT.read_text(encoding='utf-8'), re.M | re.S)
source = next(block for block in blocks if 'def configure_fresh_install(' in block)
namespace = {'__name__': 'installer_test'}
exec(compile(source, str(SCRIPT), 'exec'), namespace)
configure = namespace['configure_fresh_install']


class BootstrapTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.config = pathlib.Path(self.temp.name) / 'console.conf'
        self.original = '# preserve comment\nLISTEN_ADDR=127.0.0.1:3000\nZT_ADDRESS=http://127.0.0.1:9993\nLEGACY_DIR=\n'
        self.config.write_text(self.original)
        self.password = Mock(return_value='Test-only-password-123')
        self.restart = Mock()

    def call(self, api, fresh=True, local_only=False):
        return configure(self.config, fresh, local_only, self.password, api, self.restart)

    def test_fresh_admin_created_before_lan_open(self):
        calls = []
        def api(method, path, payload=None):
            calls.append((method, path))
            if method == 'POST':
                self.assertEqual(self.config.read_text(), self.original)
                self.assertEqual(payload['password'], self.password.return_value)
            return {'demo': False, 'setupRequired': len(calls) == 1}
        self.assertTrue(self.call(api))
        self.assertEqual(calls, [('GET', '/api/auth'), ('POST', '/api/setup'), ('GET', '/api/auth'), ('GET', '/api/auth')])
        self.assertEqual(self.config.read_text(), self.original.replace('127.0.0.1:3000', '0.0.0.0:3000'))
        self.restart.assert_called_once()

    def test_upgrade_preserves_all_configuration(self):
        for address in ('127.0.0.1:3000', '0.0.0.0:3100', '192.168.11.63:3000'):
            with self.subTest(address=address):
                original = self.original.replace('127.0.0.1:3000', address)
                self.config.write_text(original)
                api = Mock()
                self.assertFalse(self.call(api, fresh=False))
                self.assertEqual(self.config.read_text(), original)
                api.assert_not_called()
        self.password.assert_not_called()
        self.restart.assert_not_called()

    def test_local_only_keeps_bootstrap_private(self):
        api = Mock()
        self.assertFalse(self.call(api, local_only=True))
        api.assert_not_called()
        self.password.assert_not_called()
        self.assertEqual(self.config.read_text(), self.original)

    def test_existing_admin_is_not_reset(self):
        api = Mock(return_value={'demo': False, 'setupRequired': False})
        self.assertTrue(self.call(api))
        self.password.assert_not_called()
        self.assertTrue(all(call.args[0] == 'GET' for call in api.call_args_list))

    def test_invalid_password_does_not_open_lan(self):
        self.password.return_value = 'short'
        api = Mock(return_value={'demo': False, 'setupRequired': True})
        with self.assertRaisesRegex(ValueError, '密码长度'):
            self.call(api)
        self.assertEqual(self.config.read_text(), self.original)
        self.restart.assert_not_called()

    def test_setup_failure_does_not_open_lan(self):
        api = Mock(side_effect=[{'demo': False, 'setupRequired': True}, OSError('setup failed')])
        with self.assertRaises(OSError):
            self.call(api)
        self.assertEqual(self.config.read_text(), self.original)
        self.restart.assert_not_called()

    def test_unconfirmed_admin_does_not_open_lan(self):
        api = Mock(return_value={'demo': False, 'setupRequired': True})
        with self.assertRaisesRegex(ValueError, '初始化未完成'):
            self.call(api)
        self.assertEqual(self.config.read_text(), self.original)

    def test_invalid_service_response_does_not_open_lan(self):
        for status in ({}, {'demo': True, 'setupRequired': False}, {'demo': False, 'setupRequired': 'false'}):
            with self.subTest(status=status), self.assertRaises(ValueError):
                self.call(Mock(return_value=status))
        self.assertEqual(self.config.read_text(), self.original)

    def test_restart_failure_restores_loopback(self):
        self.restart.side_effect = [OSError('restart failed'), None]
        with self.assertRaises(OSError):
            self.call(Mock(return_value={'demo': False, 'setupRequired': False}))
        self.assertEqual(self.config.read_text(), self.original)
        self.assertEqual(self.restart.call_count, 2)

    def test_health_failure_restores_loopback(self):
        api = Mock(side_effect=[{'demo': False, 'setupRequired': False},
                               {'demo': False, 'setupRequired': False}, OSError('not ready')])
        with self.assertRaises(OSError):
            self.call(api)
        self.assertEqual(self.config.read_text(), self.original)
        self.assertEqual(self.restart.call_count, 2)

    def test_unexpected_listen_is_not_overwritten(self):
        original = self.original.replace('127.0.0.1:3000', '127.0.0.1:3100')
        self.config.write_text(original)
        with self.assertRaises(ValueError):
            self.call(Mock())
        self.assertEqual(self.config.read_text(), original)

    @unittest.skipUnless(os.name == 'posix', 'Unix password file permissions')
    def test_password_file_permissions_and_symlink(self):
        path = pathlib.Path(self.temp.name) / 'initial-password'
        path.write_text('Test-only-password-123\n')
        path.chmod(0o600)
        with patch.dict(os.environ, {'SUDO_UID': str(os.getuid())}):
            self.assertEqual(namespace['read_initial_password'](str(path)), 'Test-only-password-123')
        path.chmod(0o644)
        with self.assertRaisesRegex(ValueError, '权限'):
            namespace['read_initial_password'](str(path))
        path.chmod(0o600)
        link = pathlib.Path(self.temp.name) / 'linked-password'
        link.symlink_to(path)
        with self.assertRaises(OSError):
            namespace['read_initial_password'](str(link))


if __name__ == '__main__':
    unittest.main()
