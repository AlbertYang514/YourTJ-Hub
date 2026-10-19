"""End a cycle from a completed formal request, independently of Apple review.

The publisher seals its cycle/request binding before exiting. The main controller
reads that immutable artifact only after GitHub reports the whole run successful.
Editable deployment receipts alone cannot select which cycle to unlock.
"""
from copy import deepcopy
import io
import json
import os
from pathlib import Path
import subprocess
import tempfile
import zipfile

from controller import authorize, verify_reservation
from model import require
from state import latest_receipt


def write_result():
    result = {key: os.environ[env] for key, env in {
        'cycle': 'CYCLE_ID', 'candidate': 'CANDIDATE', 'source': 'SOURCE_SHA',
        'approvedHead': 'APPROVED_HEAD', 'contentDigest': 'CONTENT_DIGEST',
        'run': 'GITHUB_RUN_ID', 'attempt': 'GITHUB_RUN_ATTEMPT', 'controllerSha': 'GITHUB_SHA',
    }.items()}
    result['channels'] = json.loads(os.environ['CHANNELS'])
    folder = Path('.release-completion')
    folder.mkdir(exist_ok=True)
    (folder / 'completion.json').write_text(json.dumps(result) + '\n')


def read_result(github, run_id):
    require(str(run_id).isdigit(), 'A formal release run ID is required')
    run = github.api(f'actions/runs/{run_id}')
    require(run.get('path') in {'.github/workflows/release-publish.yml', '.github/workflows/release-recover.yml'}
            and run.get('event') in {'push', 'workflow_dispatch'} and run.get('head_branch') == 'main'
            and run.get('head_repository', {}).get('full_name', '').lower() == github.repository.lower()
            and run.get('status') == 'completed' and run.get('conclusion') == 'success',
            'Only a completed successful trusted-main formal release can finish a cycle')
    attempt = str(run['run_attempt'])
    artifacts = [a for a in github.pages(f'actions/runs/{run_id}/artifacts', 'artifacts')
                 if a['name'] == f'release-completion-{attempt}']
    require(len(artifacts) == 1 and not artifacts[0]['expired'] and artifacts[0]['size_in_bytes'] < 100_000,
            'Release completion seal missing/expired; inspect the successful release run')
    archive = subprocess.check_output(['gh', 'api',
        f'repos/{github.repository}/actions/artifacts/{artifacts[0]["id"]}/zip'])
    with zipfile.ZipFile(io.BytesIO(archive)) as zipped:
        require(zipped.namelist() == ['completion.json'] and zipped.getinfo('completion.json').file_size < 50_000,
                'Invalid release completion archive')
        result = json.loads(zipped.read('completion.json'))
    require(result['run'] == str(run_id) and result['attempt'] == attempt
            and result['controllerSha'] == run['head_sha'], 'Release completion replay or identity mismatch')
    return result


def complete_release(github, state, run_id, now):
    result = read_result(github, run_id)
    cycle_id = result['cycle']
    if not cycle_id:  # Restricted historical recovery has no release cycle to end.
        return state, 'historical-release'
    require(cycle_id in state['cycles'], 'Release completion refers to an unknown cycle')
    cycle = state['cycles'][cycle_id]
    if cycle.get('completion', {}).get('candidate') == result['candidate']:
        return state, 'already-completed'
    if cycle['phase'] != 'testing' or state['active'] != cycle_id:
        return state, 'superseded'
    require(cycle['source'] == result['source'], 'Release source no longer matches its cycle')
    with tempfile.TemporaryDirectory() as folder:
        manifest, binding = authorize(result['candidate'], github, Path(folder))
        require(manifest['sourceSha'] == result['source'] and manifest['channels'] == result['channels']
                and all(binding.get(key) == result[key] for key in ['approvedHead', 'contentDigest']),
                'Release completion does not match the reviewed request')
        verify_reservation(manifest, binding, github)
    channels = result['channels']
    require(channels and set(channels) <= set(cycle['channels']), 'Release targets exceed the tested cycle')
    qa = ['qa'] + (['productionQa'] if any(c != 'web' for c in channels) else [])
    require(all(cycle[key] and cycle[key]['source'] == result['source'] for key in qa),
            'Release completion requires the exact source QA')
    # Recover can succeed for one channel while another channel of this SAME
    # request remains failed. Other requests/platforms in the cycle are irrelevant.
    for channel in channels:
        receipt = latest_receipt(github, result['candidate'], channel)
        if not receipt or not receipt['status'] or receipt['status']['state'] != 'success':
            return state, 'incomplete'
        deployment = receipt['deployment']
        require(deployment.get('sha') == result['source']
            and deployment.get('payload', {}).get('details', {}).get('cycleId') == cycle_id and all(
            deployment.get('payload', {}).get('binding', {}).get(key) == result[key]
            for key in ['approvedHead', 'contentDigest']), 'Release receipt source/review mismatch')
    state = deepcopy(state)
    cycle = state['cycles'][cycle_id]
    cycle.update(completion=result | {'at': now}, admitted={})
    if cycle['kind'] == 'hotfix':
        cycle['phase'] = 'backflow'
    else:
        cycle.update(phase='closed', frozen=False)
        state['active'] = None
    state['lastEvent'] = {'action': 'complete', 'cycle': cycle_id, 'actor': 'release-workflow',
                          'at': now, 'note': f"Formal request {result['candidate']} completed in run {run_id}"}
    return state, cycle['phase']


if __name__ == '__main__':
    write_result()
