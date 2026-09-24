import tempfile
import unittest
from pathlib import Path
from release import check_releases, checksums, version_key


class ReleaseTests(unittest.TestCase):
    def test_tags_and_order(self):
        self.assertGreater(version_key('1.3.2-fork.10'), version_key('1.3.2-fork.9'))
        for tag in ['1.3.2', '1.3.2-fork.0', 'mod', 'Snapshot-123', '../mod', '1.3.2-fork.01']:
            with self.assertRaises(ValueError):
                version_key(tag)

    def test_no_downgrade_overwrite_or_original_release(self):
        def release(tag, draft=False):
            return {'tag_name': tag, 'draft': draft, 'prerelease': False}
        check_releases('1.3.2-fork.2', '1.3.2', [release('1.3.2-fork.1')])
        for published in [release('1.3.2-fork.2'), release('1.3.2-fork.3'), release('1.3.2'), release('1.3.2-fork.2', True)]:
            with self.assertRaises(ValueError):
                check_releases('1.3.2-fork.2', '1.3.2', [published])
        with self.assertRaises(ValueError):
            check_releases('1.3.3-fork.1', '1.3.2', [])

    def test_checksum_sidecar_and_manifest(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            (folder / 'binary').write_bytes(b'abc')
            checksums(folder)
            self.assertEqual((folder / 'binary.sha256').read_text(), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n')
            first = (folder / 'SHA256SUMS').read_text()
            checksums(folder)
            self.assertEqual(first, (folder / 'SHA256SUMS').read_text())
