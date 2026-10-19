"""Successful formal requests end their own cycle without waiting for Apple review."""
import copy
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
import zipfile
from unittest.mock import Mock, patch

from cycle import can_merge, empty_state, transition
from cycle_completion import complete_release, write_result
from model import ReleaseError, REPOSITORY

SHA = 'a' * 40


class CycleCompletionTests(unittest.TestCase):
    def setUp(self):
        self.state = transition(empty_state(), 'start', 'alice', 100, cycle='42', source=SHA,
                                issue=1240, channels=['web', 'android', 'ios-testflight', 'ios-app-store'])
        self.state['cycles']['42']['qa'] = {'source': SHA}
        self.run = {'path': '.github/workflows/release-publish.yml', 'event': 'push',
                    'head_branch': 'main', 'head_sha': 'b' * 40, 'run_attempt': 1,
                    'head_repository': {'full_name': REPOSITORY},
                    'status': 'completed', 'conclusion': 'success'}
        self.manifest = {'candidateId': 'web-1.2.3', 'sourceSha': SHA, 'channels': ['web']}
        self.binding = {'approvedHead': 'c' * 40, 'contentDigest': 'd' * 64}
        self.result = {'cycle': '42', 'candidate': 'web-1.2.3', 'source': SHA, 'channels': ['web'],
                       **self.binding, 'run': '500', 'attempt': '1', 'controllerSha': 'b' * 40}
        self.artifact = {'id': 8, 'name': 'release-completion-1', 'expired': False, 'size_in_bytes': 1000}
        self.remote = Mock(repository=REPOSITORY)
        self.remote.api.return_value = self.run
        self.remote.pages.return_value = [self.artifact]
        self.receipts = {'web': self.receipt('web')}

    def receipt(self, channel, state='success', availability='submitted'):
        return {'deployment': {'sha': SHA, 'payload': {'binding': self.binding.copy(),
                 'channel': channel, 'details': {'cycleId': '42', 'availability': availability}}}, 'status': {'state': state}}

    def finish(self, state=None):
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as archive:
            archive.writestr('completion.json', json.dumps(self.result))
        with patch('cycle_completion.subprocess.check_output', return_value=stream.getvalue()), \
             patch('cycle_completion.authorize', return_value=(self.manifest, self.binding)), \
             patch('cycle_completion.verify_reservation'), \
             patch('cycle_completion.latest_receipt', side_effect=lambda gh, candidate, channel: self.receipts.get(channel)):
            return complete_release(self.remote, self.state if state is None else state, '500', 120)

    def test_web_success_closes_broad_preview_cycle_without_mobile_release(self):
        state, status = self.finish()
        self.assertEqual(status, 'closed')
        self.assertEqual(state['cycles']['42']['phase'], 'closed')
        self.assertEqual(state['cycles']['42']['completion']['channels'], ['web'])
        self.assertIsNone(state['active'])
        self.assertTrue(can_merge(state, 12, SHA))
        self.assertIsNone(state['cycles']['42']['productionQa'])
        self.assertTrue(self.state['cycles']['42']['frozen'])  # Pure state update.

    def test_ios_submission_ends_cycle_without_querying_live_apple_availability(self):
        channels = ['ios-testflight', 'ios-app-store']
        self.manifest['channels'] = self.result['channels'] = channels
        self.state['cycles']['42']['productionQa'] = {'source': SHA}
        for availability in ['submitted', 'WAITING_FOR_REVIEW', 'IN_REVIEW']:
            with self.subTest(availability=availability):
                self.receipts = {channel: self.receipt(channel, availability=availability) for channel in channels}
                state, status = self.finish()
                self.assertEqual(status, 'closed')
                self.assertIsNone(state['active'])
        self.assertTrue(all(call.args[0] == 'actions/runs/500' for call in self.remote.api.call_args_list))

    def test_failed_cancelled_running_fork_and_preview_runs_cannot_end_cycle(self):
        for override in [{'conclusion': 'failure'}, {'conclusion': 'cancelled'},
                         {'status': 'in_progress'}, {'head_branch': 'dev'}, {'event': 'pull_request'},
                         {'path': '.github/workflows/release-prerelease.yml'},
                         {'head_repository': {'full_name': 'other/repo'}}]:
            with self.subTest(override=override), patch.dict(self.run, override), self.assertRaises(ReleaseError):
                self.finish()
        self.assertTrue(self.state['cycles']['42']['frozen'])

    def test_recovery_of_one_channel_does_not_hide_failed_channels_in_same_request(self):
        self.run.update(path='.github/workflows/release-recover.yml', event='workflow_dispatch')
        self.manifest['channels'] = self.result['channels'] = ['android', 'ios-testflight']
        self.state['cycles']['42']['productionQa'] = {'source': SHA}
        self.receipts = {'android': self.receipt('android'), 'ios-testflight': self.receipt('ios-testflight')}
        for receipt in [None, self.receipt('ios-testflight', state='failure'),
                        self.receipt('ios-testflight', state='in_progress')]:
            self.receipts['ios-testflight'] = receipt
            state, status = self.finish()
            self.assertEqual(status, 'incomplete')
            self.assertTrue(state['cycles']['42']['frozen'])
        self.receipts['ios-testflight'] = self.receipt('ios-testflight')
        self.assertEqual(self.finish()[1], 'closed')

    def test_old_callback_never_unlocks_a_new_cycle_even_with_same_source(self):
        state, _ = self.finish()
        state = transition(state, 'start', 'bob', 121, cycle='43', source=SHA, issue=1241, channels=['web'])
        completed, status = self.finish(state)
        self.assertEqual(status, 'already-completed')
        self.assertEqual(completed['active'], '43')
        self.assertTrue(completed['cycles']['43']['frozen'])

    def test_cancelled_aborted_paused_or_changed_candidate_cannot_be_revived(self):
        for phase in ['cancelled', 'aborted', 'paused']:
            state = copy.deepcopy(self.state)
            state['cycles']['42']['phase'] = phase
            self.assertEqual(self.finish(state)[1], 'superseded')
        self.state['cycles']['42']['source'] = 'e' * 40
        with self.assertRaises(ReleaseError):
            self.finish()

    def test_missing_expired_replayed_or_rewritten_result_is_rejected(self):
        for key, value in [('run', '501'), ('attempt', '2'), ('controllerSha', 'e' * 40),
                           ('approvedHead', 'e' * 40), ('contentDigest', 'e' * 64),
                           ('source', 'e' * 40), ('channels', ['android'])]:
            with self.subTest(key=key), patch.dict(self.result, {key: value}), self.assertRaises(ReleaseError):
                self.finish()
        with patch.dict(self.artifact, expired=True), self.assertRaises(ReleaseError):
            self.finish()
        self.remote.pages.return_value = []
        with self.assertRaises(ReleaseError):
            self.finish()

    def test_receipt_for_other_source_or_review_cannot_complete(self):
        for key, value in [('sha', 'e' * 40), ('payload', {'binding': {'contentDigest': 'wrong'}})]:
            with self.subTest(key=key), patch.dict(self.receipts['web']['deployment'], {key: value}), \
                 self.assertRaises(ReleaseError):
                self.finish()

    def test_hotfix_still_requires_backflow_but_never_store_approval(self):
        self.state['cycles']['42']['kind'] = 'hotfix'
        state, status = self.finish()
        self.assertEqual(status, 'backflow')
        self.assertEqual(state['active'], '42')
        state = transition(state, 'backflow', 'alice', 130, cycle='42', source='e' * 40, note='Merged and tested')
        self.assertEqual(state['cycles']['42']['phase'], 'closed')
        self.assertIsNone(state['active'])
        self.assertIn(SHA, state['requiredHotfixes'])

    def test_finishing_after_deadline_does_not_require_another_publication(self):
        self.state['cycles']['42']['until'] = 110
        self.assertEqual(self.finish()[1], 'closed')

    def test_formal_job_writes_the_cycle_and_exact_review_binding_used_by_completion(self):
        env = {env: self.result[key] for key, env in {
            'cycle': 'CYCLE_ID', 'candidate': 'CANDIDATE', 'source': 'SOURCE_SHA',
            'approvedHead': 'APPROVED_HEAD', 'contentDigest': 'CONTENT_DIGEST',
            'run': 'GITHUB_RUN_ID', 'attempt': 'GITHUB_RUN_ATTEMPT', 'controllerSha': 'GITHUB_SHA',
        }.items()} | {'CHANNELS': json.dumps(self.result['channels'])}
        with tempfile.TemporaryDirectory() as folder, patch.dict(os.environ, env), \
             patch('cycle_completion.Path', return_value=Path(folder)):
            write_result()
            self.assertEqual(json.loads((Path(folder) / 'completion.json').read_text()), self.result)

    def test_historical_recovery_has_no_cycle_to_unlock(self):
        self.result['cycle'] = ''
        state, status = self.finish()
        self.assertEqual(status, 'historical-release')
        self.assertEqual(state, self.state)

    def test_same_sha_receipt_from_previous_cycle_cannot_complete_new_cycle(self):
        self.receipts['web']['deployment']['payload']['details']['cycleId'] = '41'
        with self.assertRaisesRegex(ReleaseError, 'receipt source/review mismatch'):
            self.finish()


if __name__ == '__main__':
    unittest.main()
