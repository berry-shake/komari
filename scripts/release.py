"""Release preflight and checksums; no writes to GitHub from this script."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys


MINIMUM_VERSION = (1, 2, 4)
NUMERIC_TAG = r"(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"


def version_key(tag):
    match = re.fullmatch(NUMERIC_TAG, tag)
    if not match:
        raise ValueError(f"Expected a numeric release tag such as 1.2.4, got {tag!r}")
    return tuple(map(int, match.groups()))


def check_releases(tag, releases):
    key = version_key(tag)
    if key < MINIMUM_VERSION:
        raise ValueError("Independent distribution versions start at 1.2.4")
    for release in releases:
        if release['tag_name'] == tag:
            raise ValueError("Release already exists (including drafts); published assets are immutable")
        if release['draft'] or release['prerelease']:
            continue
        # Legacy tags belong to the old upstream-based numbering scheme. In
        # particular, Agent 1.2.13-fork.2 must not block our first release 1.2.4.
        if re.fullmatch(NUMERIC_TAG + r"-fork\.[1-9][0-9]*", release['tag_name']):
            continue
        previous = version_key(release['tag_name'])
        if key <= previous:
            raise ValueError(f"Refusing to replace latest with an older version: {tag}")


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def prepare(tag, repository):
    version_key(tag)  # Validate before passing the ref to any external command.
    if os.environ.get('GITHUB_REF') != 'refs/heads/mod':
        raise ValueError('Run this workflow from the mod branch')
    if os.environ.get('GITHUB_REPOSITORY') != repository:
        raise ValueError('Unexpected publishing repository')
    sha = run('git', 'rev-parse', '--verify', f'refs/tags/{tag}^{{commit}}')
    subprocess.run(['git', 'merge-base', '--is-ancestor', sha, 'origin/mod'], check=True)
    pages = json.loads(run('gh', 'api', '--paginate', '--slurp', f'repos/{repository}/releases?per_page=100'))
    check_releases(tag, [release for page in pages for release in page])
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
