import tempfile
import unittest
from pathlib import Path
from release import check_releases, checksums, version_key


class ReleaseTests(unittest.TestCase):
    def test_numeric_tags_and_order(self):
        self.assertGreater(version_key('1.2.10'), version_key('1.2.9'))
        self.assertGreater(version_key('1.10.0'), version_key('1.9.99'))
        for tag in ['v1.2.4', '1.2.3-fork.4', '1.2.4-beta.1', '1.2.4+build',
                    'mod', '../mod', '1.02.4', '1.2', '1.2.4.1']:
            with self.subTest(tag=tag), self.assertRaises(ValueError):
                version_key(tag)

    def test_no_downgrade_or_overwrite(self):
        def release(tag, draft=False, prerelease=False):
            return {'tag_name': tag, 'draft': draft, 'prerelease': prerelease}
        check_releases('1.2.5', [release('1.2.4')])
        check_releases('1.3.0', [release('1.2.10')])
        check_releases('1.2.4', [release('1.2.5', prerelease=True)])
        for published in [release('1.2.4'), release('1.2.5'), release('2.0.0'),
                          release('1.2.4', True), release('1.2.4', prerelease=True)]:
            with self.subTest(published=published), self.assertRaises(ValueError):
                check_releases('1.2.4', [published])
        with self.assertRaises(ValueError):
            check_releases('1.2.3', [])

    def test_legacy_numbering_does_not_block_first_numeric_release(self):
        check_releases('1.2.4', [
            {'tag_name': tag, 'draft': False, 'prerelease': False}
            for tag in ['1.2.3-fork.3', '1.2.13-fork.2', '1.3.2-fork.1']
        ])
        # Unknown stable tags must be reviewed instead of silently changing latest.
        with self.assertRaises(ValueError):
            check_releases('1.2.4', [{'tag_name': 'unexpected', 'draft': False, 'prerelease': False}])

    def test_checksum_sidecar_and_manifest(self):
        with tempfile.TemporaryDirectory() as temp:
            folder = Path(temp)
            (folder / 'binary').write_bytes(b'abc')
            checksums(folder)
            self.assertEqual((folder / 'binary.sha256').read_text(), 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad\n')
            first = (folder / 'SHA256SUMS').read_text()
            checksums(folder)
            self.assertEqual(first, (folder / 'SHA256SUMS').read_text())
