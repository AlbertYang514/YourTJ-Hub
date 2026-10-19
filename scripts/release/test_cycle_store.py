import io
import json
import os
import unittest
import zipfile
from unittest.mock import Mock, patch
from cycle_store import load
from cycle import empty_state
from model import ReleaseError, REPOSITORY


class CycleStoreTests(unittest.TestCase):
    def fixture(self):
        github = Mock(repository=REPOSITORY)
        record = {'id': 12, 'payload': {'run': '100', 'attempt': '1'}}
        run = {'path': '.github/workflows/release-control.yml', 'event': 'workflow_dispatch',
               'head_branch': 'main', 'head_sha': 'a'*40, 'head_repository': {'full_name': REPOSITORY}}
        artifact = {'id': 8, 'name': 'release-state-1', 'expired': False, 'size_in_bytes': 100}
        sealed = {'deploymentId': 12, 'run': '100', 'attempt': '1', 'controllerSha': 'a'*40, 'state': empty_state()}
        github.pages.side_effect = lambda endpoint, *args: [record] if endpoint.startswith('deployments?') else [artifact]
        github.api.return_value = run
        return github, run, artifact, sealed

    def archive(self, sealed):
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as zipped:
            zipped.writestr('state.json', json.dumps(sealed))
        return stream.getvalue()

    def test_snapshot_must_match_immutable_artifact_and_deployment_id(self):
        github, run, artifact, sealed = self.fixture()
        with patch('cycle_store.subprocess.check_output', return_value=self.archive(sealed)):
            self.assertEqual(load(github), empty_state())
        for key, value in [('deploymentId', 13), ('controllerSha', 'b'*40), ('run', '101')]:
            with patch('cycle_store.subprocess.check_output', return_value=self.archive(sealed | {key: value})):
                with self.assertRaises(ReleaseError): load(github)

    def test_pr_runs_expired_or_missing_seals_never_unfreeze(self):
        github, run, artifact, sealed = self.fixture()
        for key, value in [('event', 'pull_request'), ('head_branch', 'dev'), ('path', '.github/workflows/ci.yml')]:
            with patch.dict(run, {key: value}), self.assertRaises(ReleaseError): load(github)
        artifact['expired'] = True
        with self.assertRaises(ReleaseError): load(github)
        github.pages.side_effect = [[{'id': 12, 'payload': {'run': '100', 'attempt': '1'}}], []]
        with self.assertRaises(ReleaseError): load(github)

    def test_only_same_failed_dispatch_can_recover_unsealed_state(self):
        github, run, artifact, sealed = self.fixture()
        github.pages.side_effect = [[{'id': 12, 'payload': {'run': '100', 'attempt': '1'}}], []]
        github.api.side_effect = [run, {'conclusion': 'failure'}]
        with patch.dict(os.environ, GITHUB_RUN_ID='100', GITHUB_RUN_ATTEMPT='2'):
            self.assertEqual(load(github), empty_state())


if __name__ == '__main__': unittest.main()
