#!/usr/bin/env python3
"""Public, non-authoritative projection for installed previews and testers.

Only source/cycle metadata is public; QA notes, channel facts and Apple responses
remain in the sealed controller state. This file is never publication authority.
"""
import json
import os
from datetime import datetime, timezone
from pathlib import Path
import tempfile
import time
from cycle import current
from cycle_store import load
from github import GitHub, run
from model import require

TAG = 'preview-status'


def project(state, now):
    c = current(state)
    active = None
    if c:
        active = {key: c[key] for key in ['id', 'source', 'owner', 'issue', 'phase', 'until']}
        active['artifacts'] = {purpose: sorted(items) for purpose, items in c.get('previews', {}).items()}
        active['runs'] = {purpose: sorted(set(items.values())) for purpose, items in c.get('previews', {}).items()}
    return {'schema': 1, 'updatedAt': now, 'active': active}


def notes(status, repository):
    base = f'https://github.com/{repository}'
    c = status['active']
    body = ['# Preview candidate status', '',
            'Testing builds may remain downloadable after a candidate changes. Check this page before regression testing.', '']
    if c:
        valid = c['phase'] == 'testing' and status['updatedAt'] < c['until']
        deadline = datetime.fromtimestamp(c['until'], timezone.utc).isoformat()
        body += [f"Source: `{c['source']}` · cycle `{c['id']}` · owner @{c['owner']}",
                 f"[Release checklist]({base}/issues/{c['issue']}) · freeze deadline: {deadline}", '',
                 'Candidate selected for testing; device QA is still required.' if valid else
                 'This candidate is expired or paused. Contact the owner before using it for acceptance.', '',
                 'The fixed commit may be rebuilt. A listed run is a distribution link, not a QA pass.', '']
        for purpose, runs in c['runs'].items():
            body += [f"- {purpose}: " + ', '.join(f'[run {r}]({base}/actions/runs/{r})' for r in runs)]
    else:
        body += ['There is no active preview candidate. Previously installed previews are no longer the current regression target.']
    body += ['', 'To leave testing, install the current production app from the normal App Store or Android download entry. '
             'A lower Android version code may require uninstalling the preview first; preserve local-only data before uninstalling. '
             'The preview does not switch servers or install a production app automatically.',
             '', f'[App Store](https://apps.apple.com/app/yourtj/id6809457637) · [Android downloads]({base}/releases/tag/mobile-latest)',
             '', f"Status updated: {datetime.fromtimestamp(status['updatedAt'], timezone.utc).isoformat()}. "
             'If this page or the app cannot verify freshness, ask the release owner before testing.']
    return '\n'.join(body) + '\n'


def main():
    require(os.environ['GITHUB_REF'] == 'refs/heads/main', 'Preview status is maintained by trusted main')
    github = GitHub()
    status = project(load(github), int(time.time()))
    body = notes(status, github.repository)
    release = github.api(f'releases/tags/{TAG}', missing=True)
    data = {'name': 'Preview candidate status', 'body': body, 'draft': False,
            'prerelease': True, 'make_latest': 'false'}
    if release:
        require(release['prerelease'] and not release['draft'], 'Preview status must remain an isolated prerelease')
        github.api(f"releases/{release['id']}", method='PATCH', data=data)
    else:
        github.api('releases', method='POST', data=data | {'tag_name': TAG, 'target_commitish': os.environ['GITHUB_SHA']})
    with tempfile.TemporaryDirectory() as folder:
        path = Path(folder) / 'preview-status.json'
        path.write_text(json.dumps(status, ensure_ascii=False) + '\n')
        run('gh', 'release', 'upload', TAG, str(path), '--repo', github.repository, '--clobber')


if __name__ == '__main__': main()
