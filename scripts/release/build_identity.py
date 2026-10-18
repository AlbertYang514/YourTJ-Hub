"""Automatic build-number reservations, shared by preview and production.

Call reserve_number only under the release-mobile-identities concurrency lock.
These annotated tags reserve platform numbers even if a build/upload later fails.
"""
import json
from model import require


def reserve_number(github, source, version, number, environment):
    tag = f'mobile-build-{number}'
    require(github.api(f'git/ref/tags/{tag}', missing=True) is None, 'Build number already consumed; re-plan')
    annotation = github.api('git/tags', method='POST', data={
        'tag': tag, 'object': source, 'type': 'commit',
        'message': json.dumps({'version': version, 'buildNumber': number, 'environment': environment})})
    github.api('git/refs', method='POST', data={'ref': 'refs/tags/' + tag, 'sha': annotation['sha']})


def verify_production_number(github, manifest):
    if manifest['product'] != 'mobile':
        return
    marker = github.api(f"git/ref/tags/mobile-build-{manifest['buildNumber']}", missing=True)
    if marker is None:
        # Existing immutable formal releases predate number reservations. This
        # exception cannot allocate a fresh formal version from a preview number.
        formal = github.api(f"git/ref/tags/{manifest['tag']}", missing=True)
        require(formal and formal['object']['type'] == 'tag', 'New mobile releases require a production build reservation')
        return
    require(marker['object']['type'] == 'tag', 'Build reservation must be annotated')
    annotation = github.api(f"git/tags/{marker['object']['sha']}")
    metadata = json.loads(annotation['message'])
    require(annotation['object']['type'] == 'commit' and annotation['object']['sha'] == manifest['sourceSha']
            and metadata.get('environment') == 'production' and metadata.get('version') == manifest['version']
            and metadata.get('buildNumber') == manifest['buildNumber'],
            'Preview/other-source build numbers cannot be used for formal publication or App Store promotion')
