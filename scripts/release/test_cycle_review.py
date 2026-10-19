"""Regression coverage for hotfix admission, queued merges and transition recovery."""
import os
from pathlib import Path
import re
import tempfile
import unittest
from unittest.mock import Mock, patch
from cycle import empty_state, transition
from cycle_control import main, refresh_checks
from model import ReleaseError


class CycleReviewTests(unittest.TestCase):
    def dispatch(self, action='hotfix', baselines=None, groups=(), serialized='true'):
        remote = Mock()
        remote.api.return_value = {'state': 'open', 'object': {'sha': 'a' * 40}}
        remote.pages.return_value = list(groups)
        with tempfile.NamedTemporaryFile() as summary, patch.dict(os.environ, {
                'GITHUB_ACTOR': 'alice', 'GITHUB_TRIGGERING_ACTOR': 'alice', 'GITHUB_RUN_ID': '42',
                'ACTION': action, 'CYCLE_ID': '', 'SOURCE_SHA': 'a' * 40, 'BASE_SHA': 'b' * 40,
                'ISSUE': '1240', 'CHANNELS': 'web,android', 'NOTE': 'Fix outage',
                'PRODUCTION_SERIALIZED': serialized, 'GITHUB_STEP_SUMMARY': summary.name}), \
             patch('sys.argv', ['cycle_control.py', 'change']), \
             patch('cycle_control.GitHub', return_value=remote), patch('cycle_control.human'), \
             patch('cycle_control.load', return_value=empty_state()), patch('cycle_control.fetch_source'), \
             patch('cycle_control.git'), patch('cycle_control.reservations', return_value=[]), \
             patch('cycle_control.baselines', return_value=baselines or {
                 'web': {'sourceSha': 'b' * 40}, 'android': {'sourceSha': 'b' * 40}}), \
             patch('cycle_control.output'), patch('cycle_control.save') as save:
            main()
            return save.call_args.args[1]

    def test_hotfix_rejects_another_selected_channels_different_or_unknown_baseline(self):
        for other in ['c' * 40, None]:
            with self.subTest(other=other), self.assertRaisesRegex(ReleaseError, 'Every hotfix channel'):
                self.dispatch(baselines={'web': {'sourceSha': 'b' * 40}, 'android': {'sourceSha': other}})
        self.assertEqual(self.dispatch()['active'], '42')

    def test_hotfix_cannot_take_over_without_the_production_queue(self):
        with self.assertRaisesRegex(ReleaseError, 'production publication queue'):
            self.dispatch(serialized='false')

    def test_freeze_cannot_start_with_an_existing_merge_group(self):
        for action in ['start', 'hotfix']:
            with self.subTest(action=action), self.assertRaisesRegex(ReleaseError, 'merge queue'):
                self.dispatch(action=action, groups=[{'ref': 'refs/heads/gh-readonly-queue/dev/pr-1',
                                                     'object': {'sha': 'd' * 40}}])

    def test_transition_refresh_revokes_previously_green_group_and_restores_after_cancel(self):
        remote = Mock()
        group = {'ref': 'refs/heads/gh-readonly-queue/dev/pr-1', 'object': {'sha': 'd' * 40}}
        pr = {'number': 1, 'base': {'ref': 'dev'}, 'state': 'open', 'head': {'sha': 'c' * 40}}
        remote.pages.side_effect = lambda endpoint: [pr] if endpoint.startswith('pulls?') else [group]
        state = transition(empty_state(), 'start', 'alice', 100, cycle='42', source='a' * 40,
                           issue=1240, channels=['web'])
        for expected, snapshot in [('failure', state), ('success', empty_state())]:
            remote.api.reset_mock()
            refresh_checks(remote, snapshot)
            posted = {c.kwargs['data']['head_sha']: c.kwargs['data']['conclusion']
                      for c in remote.api.call_args_list}
            self.assertEqual(posted, {'c' * 40: expected, 'd' * 40: expected})

    def test_failed_projection_is_a_separate_rerunnable_job(self):
        workflow = (Path(__file__).resolve().parents[2] / '.github/workflows/release-control.yml').read_text()
        jobs = dict(re.findall(r'^  ([\w-]+):\n(.*?)(?=^  [\w-]+:|\Z)',
                               workflow.split('\njobs:\n')[1], re.M | re.S))
        for name in ['control', 'refresh']:
            self.assertNotIn('cycle_control.py checks', jobs[name])
            self.assertNotIn('preview_status.py', jobs[name])
            self.assertIn('Seal release state', jobs[name])
        self.assertIn('needs: [control, refresh]', jobs['project'])
        self.assertIn('cycle_control.py checks', jobs['project'])
        self.assertIn('preview_status.py', jobs['project'])


if __name__ == '__main__':
    unittest.main()
