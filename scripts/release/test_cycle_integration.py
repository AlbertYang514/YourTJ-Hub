import unittest
import json
import os
import tempfile
from unittest.mock import Mock, patch
import workflow
from cli import parser, execute
from cycle_control import production_gate, human, reviewed_preview_source, publication_results, main as control
from model import ReleaseError, next_identity, REPOSITORY
from build_identity import verify_production_number
from cycle import empty_state, transition
from test_model import candidate
SHA = 'a' * 40


class CycleIntegrationTests(unittest.TestCase):
    def test_all_production_entrypoints_fail_before_side_effects_without_qa(self):
        manifest = candidate()
        for command in ['authorize', 'reserve', 'start', 'verify-publication', 'verify-deploy']:
            with self.subTest(command=command), patch('sys.argv', ['workflow.py', command]), \
                 patch('workflow.authorize', return_value=(manifest, {})), \
                 patch('workflow.production_gate', side_effect=ReleaseError('QA missing')) as gate, \
                 patch('workflow.reserve') as reserve, patch('workflow.record') as record, \
                 patch('workflow.emit_outputs') as emit:
                with self.assertRaisesRegex(ReleaseError, 'QA missing'): workflow.main()
                gate.assert_called_once()
                reserve.assert_not_called(); record.assert_not_called(); emit.assert_not_called()

    def test_preview_and_production_consume_unique_numbers_without_consuming_versions(self):
        version, number = next_identity('mobile', 'patch', [
            {'tag': 'mobile-v1.2.3', 'buildNumber': 24}, {'tag': 'mobile-build-30', 'buildNumber': 30}])
        self.assertEqual((version, number), ('1.2.4', 31))

    def test_dev_ipa_number_cannot_be_relabelled_by_a_reviewed_release_pr(self):
        remote = Mock()
        manifest = candidate()
        metadata = {'environment': 'production', 'version': manifest['version'], 'buildNumber': manifest['buildNumber']}
        annotation = {'object': {'type': 'commit', 'sha': manifest['sourceSha']}, 'message': json.dumps(metadata)}
        marker = {'object': {'type': 'tag', 'sha': 'tag'}}
        remote.api.side_effect = [marker, annotation]
        verify_production_number(remote, manifest)
        for environment in ['dev-preview', 'production-preview']:
            remote.api.side_effect = [marker, annotation | {'message': json.dumps(metadata | {'environment': environment})}]
            with self.assertRaisesRegex(ReleaseError, 'Preview/other-source'):
                verify_production_number(remote, manifest)

    def test_operator_commands_are_dry_run_without_remote_calls(self):
        remote = Mock()
        for argv in [['prerelease', '--source', SHA, '--issue', '1240'],
                     ['control', '--action', 'cancel', '--cycle', '42', '--note', 'Blocked']]:
            result = execute(parser().parse_args(argv), remote)
            self.assertTrue(result['dryRun'])
            self.assertEqual(result['ref'], 'main')
        remote.assert_not_called()
        self.assertEqual(remote.mock_calls, [])

    def test_bot_or_reader_cannot_own_a_release(self):
        remote = Mock()
        for permission, kind in [('write', 'Bot'), ('read', 'User')]:
            remote.api.return_value = {'permission': permission, 'user': {'type': kind}}
            with self.assertRaises(ReleaseError): human(remote, 'actor')

    def test_candidate_edits_cannot_replace_the_main_controller_or_human_review(self):
        remote = Mock(repository=REPOSITORY)
        remote.pages.side_effect = [[{'number': 12, 'base': {'ref': 'dev'}, 'head': {
            'sha': SHA, 'repo': {'full_name': REPOSITORY}}, 'user': {'login': 'author'}}],
            [{'user': {'login': 'reviewer'}, 'state': 'APPROVED', 'commit_id': 'b'*40}]]
        remote.maintainers.return_value = {'reviewer'}
        with self.assertRaises(ReleaseError): reviewed_preview_source(remote, SHA)

    def test_main_advancing_does_not_change_the_approved_source(self):
        state = transition(empty_state(), 'start', 'alice', 100, cycle='42', source=SHA,
                           issue=1240, channels=['web'])
        state['cycles']['42']['qa'] = state['cycles']['42']['productionQa'] = {'source': SHA}
        remote = Mock()
        with patch('cycle_control.load', return_value=state), patch('cycle_control.time.time', return_value=110), \
             patch('cycle_control.fetch_source') as fetch, patch('cycle_control.includes_hotfixes') as hotfix, \
             patch('cycle_control.reviewed_main_pr') as reviewed:
            production_gate(remote, SHA, ['web'])
            fetch.assert_called_once_with(SHA)
            reviewed.assert_called_once_with(remote, SHA)
            hotfix.assert_called_once_with(state, SHA)
            with self.assertRaises(ReleaseError): production_gate(remote, 'c'*40, ['web'])

    def test_hotfix_revision_stays_on_actual_channel_base_instead_of_dev(self):
        state = transition(empty_state(), 'hotfix', 'alice', 100, cycle='42', source=SHA,
                           base='b'*40, note='Outage', issue=1240, channels=['android'])
        state['requiredHotfixes'] = ['d'*40]  # Another channel can have newer changes.
        remote = Mock()
        remote.api.return_value = {'object': {'sha': 'e'*40}}
        remote.pages.return_value = []
        with tempfile.NamedTemporaryFile() as summary, patch.dict(os.environ, {
                'CYCLE_ID': '42', 'GITHUB_RUN_ID': '43', 'GITHUB_ACTOR': 'alice',
                'ACTION': 'advance', 'SOURCE_SHA': 'c'*40, 'NOTE': 'Reviewed blocker fix',
                'GITHUB_STEP_SUMMARY': summary.name}), \
             patch('sys.argv', ['cycle_control.py', 'change']), \
             patch('cycle_control.GitHub', return_value=remote), patch('cycle_control.human'), \
             patch('cycle_control.load', return_value=state), patch('cycle_control.fetch_source'), \
             patch('cycle_control.includes_hotfixes') as required, patch('cycle_control.git') as ancestry, \
             patch('cycle_control.time.time', return_value=110), patch('cycle_control.output'), \
             patch('cycle_control.save') as save:
            control()
            ancestry.assert_called_once_with('merge-base', '--is-ancestor', 'b'*40, 'c'*40)
            required.assert_not_called()
            self.assertEqual(save.call_args.args[1]['cycles']['42']['source'], 'c'*40)

    def test_channel_hotfix_publication_does_not_pull_unrelated_dev_hotfixes(self):
        state = transition(empty_state(), 'hotfix', 'alice', 100, cycle='42', source=SHA,
                           base='b'*40, note='Outage', issue=1240, channels=['android'])
        state['cycles']['42']['qa'] = state['cycles']['42']['productionQa'] = {'source': SHA}
        state['requiredHotfixes'] = ['d'*40]
        with patch('cycle_control.load', return_value=state), patch('cycle_control.time.time', return_value=110), \
             patch('cycle_control.fetch_source'), patch('cycle_control.reviewed_main_pr'), \
             patch('cycle_control.includes_hotfixes') as required:
            production_gate(Mock(), SHA, ['android'])
            required.assert_not_called()

    def test_store_wait_can_unfreeze_but_not_close_and_other_channels_must_be_live(self):
        cycle = {'source': SHA, 'channels': ['web', 'android', 'ios-app-store']}
        tags = [{'tag': 'mobile-v1.2.3', 'buildNumber': 24, 'sourceSha': SHA}]
        apple = {'storeVersions': [{'version': '1.2.3', 'buildNumber': 24, 'state': 'WAITING_FOR_REVIEW'}]}
        remote = Mock()
        remote.pages.side_effect = lambda path: [{'state': 'success'}] if path.endswith('/statuses') else [{'id': 1}]
        with patch('cycle_control.reservations', return_value=tags), \
             patch('cycle_control.baselines', return_value={'web': {'sourceSha': SHA}, 'android': {'sourceSha': SHA}}) as live:
            publication_results(remote, cycle, closing=False, apple=apple)
            with self.assertRaisesRegex(ReleaseError, 'actual availability'):
                publication_results(remote, cycle, closing=True, apple=apple)
            apple['storeVersions'][0]['state'] = 'READY_FOR_DISTRIBUTION'
            publication_results(remote, cycle, closing=True, apple=apple)
            live.return_value['android']['sourceSha'] = 'b'*40
            with self.assertRaisesRegex(ReleaseError, 'Every required channel'):
                publication_results(remote, cycle, closing=False, apple=apple)
            # A later release does not prevent closing this source's successful history.
            publication_results(remote, cycle, closing=True, apple=apple)

    def test_rerunning_only_dev_deploy_cannot_preserve_previous_qa(self):
        state = transition(empty_state(), 'start', 'alice', 100, cycle='42', source=SHA,
                           issue=1240, channels=['web'])
        c = state['cycles']['42']
        c['builds']['dev'] = '42'
        c['qa'] = {'source': SHA}
        remote = Mock()
        remote.api.return_value = {'object': {'sha': SHA}}
        with patch.dict(os.environ, {'CYCLE_ID': '42', 'SOURCE_SHA': SHA, 'BUILD_RUN': '42'}), \
             patch('sys.argv', ['cycle_control.py', 'check-dev']), \
             patch('cycle_control.GitHub', return_value=remote), patch('cycle_control.load', return_value=state), \
             patch('cycle_control.time.time', return_value=110):
            with self.assertRaisesRegex(ReleaseError, 'QA already recorded'):
                control()


if __name__ == '__main__': unittest.main()
