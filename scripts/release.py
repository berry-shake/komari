"""Release preflight and checksums; no writes to GitHub from this script."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


def version_key(tag):
    match = re.fullmatch(r"(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)-fork\.([1-9]\d*)", tag)
    if not match:
        raise ValueError(f"Expected a distribution tag such as 1.2.3-fork.1, got {tag!r}")
    return tuple(map(int, match.groups()))


def check_releases(tag, base, releases):
    key = version_key(tag)
    if tuple(map(int, base.split('.'))) != key[:3]:
        raise ValueError(f"This maintenance line is based on {base}")
    for release in releases:
        if release['tag_name'] == tag:
            raise ValueError("Release already exists (including drafts); published assets are immutable")
        if release['draft'] or release['prerelease']:
            continue
        # Do not publish original upstream releases alongside fork prerelease-style
        # tags: the Agent's SemVer updater would consider the upstream tag newer.
        previous = version_key(release['tag_name'])
        if previous[:3] != key[:3]:
            continue  # Other baselines are retained only for rollback.
        if key <= previous:
            raise ValueError(f"Refusing to replace latest with an older version: {tag}")


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def prepare(tag, base, repository):
    version_key(tag)  # Validate before passing the ref to any external command.
    if os.environ.get('GITHUB_REF') != 'refs/heads/mod-single-db':
        raise ValueError('Run this workflow from the mod-single-db branch')
    if os.environ.get('GITHUB_REPOSITORY') != repository:
        raise ValueError('Unexpected publishing repository')
    sha = run('git', 'rev-parse', '--verify', f'refs/tags/{tag}^{{commit}}')
    subprocess.run(['git', 'merge-base', '--is-ancestor', sha, 'origin/mod-single-db'], check=True)
    pages = json.loads(run('gh', 'api', '--paginate', '--slurp', f'repos/{repository}/releases?per_page=100'))
    check_releases(tag, base, [release for page in pages for release in page])
    if repository.endswith('/komari-web'):
        theme = json.loads(run('git', 'show', f'{sha}:komari-theme.json'))
        if theme['version'] != tag:
            raise ValueError('Update komari-theme.json.version before creating the web tag')
    with open(os.environ['GITHUB_OUTPUT'], 'a') as output:
        output.write(f'sha={sha}\n')
    print(f'Validated {repository}@{tag}: {sha}')


def checksums(directory):
    folder = Path(directory)
    files = sorted(p for p in folder.iterdir() if p.is_file() and p.name != 'SHA256SUMS' and not p.name.endswith('.sha256'))
    if not files:
        raise ValueError('No release artifacts found')
    manifest = []
    for artifact in files:
        with artifact.open('rb') as stream:
            digest = hashlib.file_digest(stream, 'sha256').hexdigest()
        artifact.with_name(artifact.name + '.sha256').write_text(digest + '\n')
        manifest.append(f'{digest}  {artifact.name}\n')
    (folder / 'SHA256SUMS').write_text(''.join(manifest))


if __name__ == '__main__':
    if sys.argv[1] == 'prepare':
        prepare(*sys.argv[2:])
    elif sys.argv[1] == 'checksums':
        checksums(sys.argv[2])
    else:
        raise SystemExit('Unknown command')
