#!/usr/bin/env python3
"""Reserve one preview identity after the source checks; no production aliases."""
import json
import os
import re
from pathlib import Path
from build_identity import reserve_number
from cycle_store import load
from cycle import current, live
from cycle_control import output, fetch_source, authorize_preview_source
from github import GitHub, git
from model import next_identity, require
from state import reservations
import time


def main():
    github = GitHub()
    c = current(load(github))
    purpose = os.environ['PURPOSE']
    require(c and c['id'] == os.environ['CYCLE_ID'] and c['builds'].get(purpose) == os.environ['GITHUB_RUN_ID'],
            'Preview was superseded')
    live(c, int(time.time()))
    fetch_source(c['source'])
    authorize_preview_source(github, c, purpose, os.environ['GITHUB_RUN_ID'])
    pubspec = git('show', f"{c['source']}:apps/mobile/packages/forum_app/pubspec.yaml")
    floor = re.search(r'(?m)^version:\s*(\d+\.\d+\.\d+)\+([1-9]\d*)\s*$', pubspec)
    require(floor, 'Mobile version/build floor missing')
    version, number = next_identity('mobile', 'patch', reservations(), floor[1], int(floor[2]))
    reserve_number(github, c['source'], version, number, purpose + '-preview')
    tag = f"preview-{c['id']}-{os.environ['GITHUB_RUN_ID']}-{os.environ['GITHUB_RUN_ATTEMPT']}-{purpose}"
    annotation = github.api('git/tags', method='POST', data={
        'tag': tag, 'object': c['source'], 'type': 'commit',
        'message': json.dumps({'source': c['source'], 'cycle': c['id'], 'version': version,
                               'buildNumber': number, 'environment': purpose, 'preview': True})})
    github.api('git/refs', method='POST', data={'ref': 'refs/tags/' + tag, 'sha': annotation['sha']})
    output({'version': version, 'number': number, 'tag': tag})
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as out:
        out.write(f"{purpose} preview: `{c['source']}` · {version} ({number})\n\n"
                  "Android downloads stay on a GitHub prerelease; TestFlight uses the dedicated internal QA group.\n")


if __name__ == '__main__':
    main()
