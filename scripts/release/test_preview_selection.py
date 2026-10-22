import unittest
from unittest.mock import Mock
from cycle_control import qa_builds, human
from preview_checks import verify
from model import ReleaseError, REPOSITORY


class PreviewSelectionTests(unittest.TestCase):
    def test_web_only_skips_mobile_jobs_but_cannot_skip_selected_deploy(self):
        source = {k: {'result': 'success'} for k in ['begin', 'backend', 'frontend', 'contract', 'govulncheck']}
        source.update({k: {'result': 'skipped'} for k in ['mobile', 'native']})
        verify('source', ['web'], source)
        results = {k: {'result': 'success'} for k in ['begin', 'validated', 'dev']}
        results.update({k: {'result': 'skipped'} for k in ['identity', 'android', 'ios']})
        verify('distribution', ['web'], results)
        with self.assertRaises(ReleaseError): verify('distribution', ['web', 'android'], results)
        results['dev']['result'] = 'skipped'
        with self.assertRaises(ReleaseError): verify('distribution', ['web'], results)

    def test_qa_reuses_successful_web_even_if_another_job_failed(self):
        remote = Mock(repository=REPOSITORY)
        remote.api.return_value = {'path': '.github/workflows/release-prerelease.yml',
            'head_branch': 'main', 'event': 'workflow_dispatch', 'head_repository': {'full_name': REPOSITORY},
            'status': 'completed', 'conclusion': 'failure'}
        remote.pages.return_value = [
            {'id': 1, 'name': 'validated', 'conclusion': 'success'},
            {'id': 2, 'name': 'dev / deploy', 'conclusion': 'success'},
            {'id': 3, 'name': 'android / preview', 'conclusion': 'failure'}]
        cycle = {'channels': ['web'], 'previews': {'dev': {'web': '50'}}}
        self.assertEqual(qa_builds(remote, cycle, 'dev'), {'web': '50'})
        cycle['channels'].append('android')
        with self.assertRaises(ReleaseError): qa_builds(remote, cycle, 'dev')
        cycle['previews']['dev']['android'] = '50'
        with self.assertRaises(ReleaseError): qa_builds(remote, cycle, 'dev')
        # A failed rerun supersedes an earlier successful job.
        cycle['channels'] = ['web']
        del cycle['previews']['dev']['android']
        remote.pages.return_value.append({'id': 4, 'name': 'dev / deploy', 'conclusion': 'failure'})
        with self.assertRaises(ReleaseError): qa_builds(remote, cycle, 'dev')

    def test_maintainer_is_not_repository_admin(self):
        remote = Mock()
        for permission in ['write', 'maintain']:
            remote.api.return_value = {'permission': permission, 'user': {'type': 'User'}}
            with self.assertRaises(ReleaseError): human(remote, 'bob', admin=True)
        remote.api.return_value = {'permission': 'admin', 'user': {'type': 'User'}}
        human(remote, 'bob', admin=True)


if __name__ == '__main__': unittest.main()
