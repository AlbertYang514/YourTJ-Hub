"""Tamper-evident cycle snapshots sealed by a trusted-main Actions artifact.

Deployment payloads and issue comments alone are not authority: other workflows can
write deployments. The immutable artifact binds the deployment ID to the controller
run. A missing/expired seal fails closed, never silently unfreezes dev.
"""
import io
import json
import os
from pathlib import Path
import subprocess
import zipfile
from cycle import empty_state
from model import require

ENVIRONMENT = 'release-cycle'
WORKFLOWS = {'.github/workflows/release-control.yml', '.github/workflows/release-prerelease.yml'}


def load(github, records=None):
    records = github.pages(f'deployments?environment={ENVIRONMENT}') if records is None else records
    if not records:
        return empty_state()
    record = records[0]
    payload = record.get('payload') or {}
    run_id, attempt = str(payload.get('run', '')), str(payload.get('attempt', ''))
    require(run_id.isdigit() and attempt.isdigit(), 'Unsealed release state; reconcile controller run')
    run = github.api(f'actions/runs/{run_id}')
    require(run.get('path') in WORKFLOWS and run.get('event') == 'workflow_dispatch'
            and run.get('head_branch') == 'main'
            and run.get('head_repository', {}).get('full_name', '').lower() == github.repository.lower(),
            'Release state was not written by the trusted-main controller')
    name = f'release-state-{attempt}'
    artifacts = [a for a in github.pages(f'actions/runs/{run_id}/artifacts', 'artifacts') if a['name'] == name]
    # If saving succeeded but upload failed, a rerun of that same trusted dispatch
    # may replay its unchanged inputs from the preceding sealed snapshot.
    if not artifacts and run_id == os.environ.get('GITHUB_RUN_ID') and int(attempt) < int(os.environ.get('GITHUB_RUN_ATTEMPT', '1')):
        previous = github.api(f'actions/runs/{run_id}/attempts/{attempt}')
        require(previous.get('conclusion') in {'failure', 'cancelled', 'timed_out'}, 'Previous state writer is still running')
        return load(github, records[1:])
    require(len(artifacts) == 1 and not artifacts[0]['expired'] and artifacts[0]['size_in_bytes'] < 2_000_000,
            'Release state seal missing/expired; inspect the controller run before retrying')
    archive = subprocess.check_output(['gh', 'api', f'repos/{github.repository}/actions/artifacts/{artifacts[0]["id"]}/zip'])
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
        require(zipped.namelist() == ['state.json'] and zipped.getinfo('state.json').file_size < 1_000_000,
                'Invalid release state archive')
        sealed = json.loads(zipped.read('state.json'))
    require(sealed['deploymentId'] == record['id'] and sealed['run'] == run_id
            and sealed['attempt'] == attempt and sealed['controllerSha'] == run['head_sha'],
            'Release state replay or identity mismatch')
    require(sealed['state'].get('schema') == 1, 'Unsupported release state')
    return sealed['state']


def save(github, state):
    require(os.environ['GITHUB_REF'] == 'refs/heads/main', 'Controller must run on main')
    run, attempt = os.environ['GITHUB_RUN_ID'], os.environ['GITHUB_RUN_ATTEMPT']
    deployment = github.api('deployments', method='POST', data={
        'ref': os.environ['GITHUB_SHA'], 'environment': ENVIRONMENT, 'auto_merge': False,
        'required_contexts': [], 'production_environment': False, 'transient_environment': False,
        'description': f"{state['lastEvent']['action']} / {state['lastEvent']['cycle']}",
        'payload': {'run': run, 'attempt': attempt}})
    Path('.release-state').mkdir(exist_ok=True)
    Path('.release-state/state.json').write_text(json.dumps({
        'deploymentId': deployment['id'], 'controllerSha': os.environ['GITHUB_SHA'],
        'run': run, 'attempt': attempt, 'state': state}, ensure_ascii=False, indent=2) + '\n')
    github.api(f"deployments/{deployment['id']}/statuses", method='POST', data={
        'state': 'success', 'auto_inactive': False,
        'log_url': f'https://github.com/{github.repository}/actions/runs/{run}',
        'description': 'State must be verified against its immutable Actions seal'})
