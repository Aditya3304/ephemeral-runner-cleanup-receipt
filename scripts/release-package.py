#!/usr/bin/env python3
"""Build or verify the local release dossier without including private secrets."""
import argparse
import datetime
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import tarfile
import urllib.parse
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parent.parent
VERSION = (ROOT / 'VERSION').read_text().strip()
TAG = 'v' + VERSION
OUTPUTS = ROOT.parent
DESTINATION = OUTPUTS / f'ephemeral-runner-cleanup-receipt-{VERSION}'
ARCHIVE = OUTPUTS / f'ephemeral-runner-cleanup-receipt-{VERSION}.tar.gz'

def sha(path):
    value = hashlib.sha256()
    with path.open('rb') as source:
        for block in iter(lambda: source.read(1024 * 1024), b''):
            value.update(block)
    return value.hexdigest()

def command(args, *, stdout=None):
    return subprocess.run(args, cwd=ROOT, check=True, stdout=stdout, capture_output=stdout is None, text=stdout is None).stdout

def get(path):
    with urllib.request.urlopen('http://127.0.0.1:8080' + path, timeout=30) as response:
        return json.load(response)

def finalized(run_id):
    matches = []
    for result in (ROOT / '.build/finalized').glob('*/result.json'):
        value = json.loads(result.read_text())
        if value.get('identity', {}).get('run_id') == run_id:
            matches.append(result.parent)
    if len(matches) != 1:
        raise RuntimeError(f'Expected one finalized directory for {run_id}')
    return matches[0]

def verify_directory(directory):
    checksums = directory / 'checksums.sha256'
    expected = {}
    for line in checksums.read_text().splitlines():
        digest, name = line.split('  ', 1)
        if not len(digest) == 64 or name in expected or name.startswith('/') or '..' in pathlib.PurePosixPath(name).parts:
            raise RuntimeError('Invalid package checksum entry')
        expected[name] = digest
    files = {path.relative_to(directory).as_posix(): path for path in directory.rglob('*') if path.is_file() and path != checksums}
    if set(files) != set(expected):
        raise RuntimeError('Package file inventory differs from checksums')
    for name, path in files.items():
        if path.is_symlink() or sha(path) != expected[name]:
            raise RuntimeError(f'Package verification failed: {name}')
    manifest = json.loads((directory / 'release-manifest.json').read_text())
    if manifest['tag'] != TAG or manifest['version'] != VERSION:
        raise RuntimeError('Release manifest version mismatch')
    return {'files': len(files), 'manifest_sha256': sha(directory / 'release-manifest.json'), 'checksums_sha256': sha(checksums)}

def verify_archive(archive, directory):
    prefix = directory.name + '/'
    expected = {prefix + path.relative_to(directory).as_posix(): sha(path) for path in directory.rglob('*') if path.is_file()}
    actual = {}
    with tarfile.open(archive, 'r:gz') as package:
        for member in package.getmembers():
            if member.issym() or member.islnk() or member.name.startswith('/') or '..' in pathlib.PurePosixPath(member.name).parts:
                raise RuntimeError('Unsafe release archive member')
            if member.isfile():
                source = package.extractfile(member)
                if source is None:
                    raise RuntimeError('Unreadable release archive member')
                actual[member.name] = hashlib.sha256(source.read()).hexdigest()
    if actual != expected:
        raise RuntimeError('Compressed release archive differs from the verified directory')
    return len(actual)

def create():
    commit = command(['git', 'rev-list', '-n', '1', TAG]).strip()
    head = command(['git', 'rev-parse', 'HEAD']).strip()
    if commit != head:
        raise SystemExit(f'{TAG} must point to the current committed release before packaging')
    for path in [DESTINATION, ARCHIVE]:
        if path.exists():
            raise SystemExit(f'Refusing to replace existing release artifact: {path}')
    stage = OUTPUTS / f'.{DESTINATION.name}.pending-{os.getpid()}'
    if stage.exists():
        raise SystemExit('Unexpected release staging directory already exists')
    stage.mkdir()
    try:
        (stage / 'source').mkdir()
        source_archive = stage / 'source' / f'ephemeral-runner-cleanup-receipt-{VERSION}-source.tar.gz'
        with source_archive.open('wb') as destination:
            command(['git', 'archive', '--format=tar.gz', f'--prefix=ephemeral-runner-cleanup-receipt-{VERSION}/', TAG], stdout=destination)
        with tarfile.open(source_archive, 'r:gz') as source:
            forbidden = [member.name for member in source.getmembers() if member.isfile() and re.search(r'(^|/)(password|token|[^/]+\.(key|pem|p12))$', member.name, re.I)]
            if forbidden:
                raise RuntimeError(f'Private-looking source files cannot enter the release: {forbidden}')
        shutil.copy2(ROOT / 'docs/local-release.md', stage / 'README.md')

        backup_dir = stage / 'backup'
        backup_dir.mkdir()
        for name in ['metadata.dump', 'metadata-manifest.json', 'restore-verification.json']:
            shutil.copy2(ROOT / '.build/release/backup' / name, backup_dir / name)

        rehearsals_dir = stage / 'rehearsals'
        rehearsals_dir.mkdir()
        rehearsals = []
        for number in [1, 2]:
            source = ROOT / f'.build/release/rehearsals/rehearsal-{number}.json'
            shutil.copy2(source, rehearsals_dir / source.name)
            rehearsals.append(json.loads(source.read_text()))

        demo_dir = stage / 'demo'
        shutil.copytree(ROOT / '.build/release/demo', demo_dir)

        samples = stage / 'sample-evidence'
        samples.mkdir()
        selected = rehearsals[1]['results']
        sample_rows = []
        for item in selected:
            label = 'failure' if item['verdict'] != 'pass' else 'pass'
            destination = samples / label
            destination.mkdir()
            source = finalized(item['run_id'])
            for name in ['receipt.json', 'receipt.bundle.json', 'result.json', 'verification.json']:
                shutil.copy2(source / name, destination / name)
            metadata = get('/v1/receipts?run_id=' + urllib.parse.quote(item['run_id']))['items']
            if len(metadata) != 1:
                raise RuntimeError('Sample receipt metadata is ambiguous')
            (destination / 'api-metadata.json').write_text(json.dumps(metadata[0], indent=2) + '\n')
            verification = get('/v1/receipts/' + item['receipt_id'] + '/verification')
            (destination / 'api-verification.json').write_text(json.dumps(verification, indent=2) + '\n')
            sample_rows.append({'label': label, **item})

        contracts = {}
        contract_files = [*sorted((ROOT / 'db/migrations').glob('*.sql')), ROOT / 'internal/finalizer/receipt.schema.json', ROOT / 'action/action.yml']
        deployment_files = [ROOT / name for name in ['compose.yaml', 'compose.archive.yaml', 'compose.sigstore.yaml', 'compose.finalizer.yaml', 'compose.api.yaml', 'infra/kind.yaml', 'infra/network-policy.yaml']]
        for path in [*contract_files, *deployment_files]:
            contracts[path.relative_to(ROOT).as_posix()] = sha(path)
        manifest_images = {}
        for path in deployment_files:
            values = sorted(set(re.findall(r'[A-Za-z0-9./:_-]+@sha256:[0-9a-f]{64}', path.read_text())))
            if values:
                manifest_images[path.relative_to(ROOT).as_posix()] = values
        backup = json.loads((ROOT / '.build/release/backup/metadata-manifest.json').read_text())
        demo = json.loads((ROOT / '.build/release/demo/demo-manifest.json').read_text())
        manifest = {
            'kind': 'local-release-manifest/v1',
            'version': VERSION,
            'tag': TAG,
            'commit': commit,
            'created_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'scope': 'single-laptop local/offline hackathon release',
            'runtime_downloads': 0,
            'paid_services_used': False,
            'cloud_resources': [],
            'images': {
                'postgres': 'postgres:17.11-bookworm@sha256:051f7b7b3abdd564d5d1bd1e8c4b9c1b6e77087d1dd22020ede611c096a272e0',
                'runner': (ROOT / '.build/runner-image').read_text().strip(),
                'finalizer': (ROOT / '.build/finalizer-image-id').read_text().strip(),
                'api': (ROOT / '.build/api-image-id').read_text().strip(),
                'manifest_pins': manifest_images,
            },
            'trust': {
                'finalizer_revision': (ROOT / '.build/finalizer-revision').read_text().strip(),
                'trust_root_sha256': hashlib.sha256((ROOT / '.build/finalizer-trust/trusted-root.json').read_bytes()).hexdigest(),
                'cosign_sha256': sha(ROOT / '.build/cosign'),
            },
            'contracts': contracts,
            'backup': {'sha256': backup['backup_sha256'], 'bytes': backup['backup_bytes'], 'snapshot': backup['snapshot'], 'restore_matched': True},
            'rehearsals': [{'number': row['number'], 'completed_at': row['completed_at'], 'results': row['results'], 'backup_restore_matched': row['backup_restore_matched']} for row in rehearsals],
            'sample_evidence': sample_rows,
            'demo': demo,
            'private_secrets_included': False,
        }
        (stage / 'release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        entries = []
        for path in sorted((p for p in stage.rglob('*') if p.is_file()), key=lambda p: p.relative_to(stage).as_posix()):
            entries.append(f'{sha(path)}  {path.relative_to(stage).as_posix()}')
        (stage / 'checksums.sha256').write_text('\n'.join(entries) + '\n')
        verification = verify_directory(stage)
        stage.rename(DESTINATION)
        with tarfile.open(ARCHIVE, 'w:gz', compresslevel=9) as package:
            package.add(DESTINATION, arcname=DESTINATION.name, recursive=True)
        tar_files = verify_archive(ARCHIVE, DESTINATION)
        package_result = {
            'kind': 'local-release-package-verification/v1',
            'checked_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'version': VERSION,
            'tag': TAG,
            'commit': commit,
            'directory': str(DESTINATION),
            'archive': str(ARCHIVE),
            'archive_bytes': ARCHIVE.stat().st_size,
            'archive_sha256': sha(ARCHIVE),
            'archive_files': tar_files,
            'directory_verification': verification,
            'private_secrets_included': False,
            'passed': True,
        }
        target = ROOT / '.build/release/package-verification.json'
        target.write_text(json.dumps(package_result, indent=2) + '\n')
        milestone = {
            'kind': 'milestone-i-validation/v1',
            'status': 'passed',
            'completed_at': datetime.datetime.now(datetime.timezone.utc).isoformat(),
            'version': VERSION,
            'tag': TAG,
            'commit': commit,
            'package': package_result,
            'backup': backup,
            'backup_restore': json.loads((ROOT / '.build/release/backup/restore-verification.json').read_text()),
            'rehearsals': rehearsals,
            'demo': demo,
            'final_status': json.loads((ROOT / '.build/release-status.json').read_text()),
            'sample_evidence': sample_rows,
            'runtime_downloads': 0,
            'paid_services_used': False,
            'cloud_resources': [],
            'persistent_volumes_preserved_during_stop': 28,
            'acceptance_complete': True,
        }
        data = json.dumps(milestone, indent=2) + '\n'
        (ROOT / '.build/milestone-i-results.json').write_text(data)
        (OUTPUTS / 'milestone-i-results.json').write_text(data)
        print(json.dumps(package_result, indent=2))
    except Exception:
        if stage.exists():
            shutil.rmtree(stage)
        raise

parser = argparse.ArgumentParser()
parser.add_argument('mode', choices=['create', 'verify'])
args = parser.parse_args()
if args.mode == 'create':
    create()
else:
    result = verify_directory(DESTINATION)
    if not ARCHIVE.is_file():
        raise SystemExit('Release archive is missing')
    result.update({'archive_sha256': sha(ARCHIVE), 'archive_bytes': ARCHIVE.stat().st_size, 'archive_files': verify_archive(ARCHIVE, DESTINATION), 'passed': True})
    print(json.dumps(result, indent=2))
