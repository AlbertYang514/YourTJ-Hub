import unittest
from unittest.mock import Mock, patch
from cycle import empty_state, transition, authorize_source
from cycle_control import production_gate, reconcile_abort
from model import ReleaseError

SHA = 'a' * 40


class CycleFollowupsTests(unittest.TestCase):
    def start(self, channels=None):
        return transition(empty_state(), 'start', 'alice', 100, cycle='42', source=SHA,
                          issue=1240, channels=channels or ['web'])

    def test_admin_can_reassign_expired_cycle_without_old_owner(self):
        state = self.start()
        with self.assertRaises(ReleaseError):
            transition(state, 'reassign', 'bob', 999999, cycle='42', assignee='carol', note='Owner unavailable')
        state = transition(state, 'reassign', 'bob', 999999, cycle='42', assignee='carol',
                           note='Owner unavailable', admin=True)
        c = state['cycles']['42']
        self.assertEqual(c['owner'], 'carol')
        self.assertEqual(c['ownershipHistory'][-1]['previous'], 'alice')
        self.assertEqual(c['ownershipHistory'][-1]['actor'], 'bob')
        state = transition(state, 'cancel', 'carol', 999999, cycle='42', note='Stop testing')
        self.assertIsNone(state['active'])

    def test_abort_keeps_partial_results_and_revokes_qa(self):
        state = self.start()
        state['cycles']['42']['qa'] = {'source': SHA}
        facts = {'web': {'receiptState': 'failure', 'actualSource': None}}
        state = transition(state, 'abort', 'alice', 999999, cycle='42', note='Stop; incident assigned', outcome=facts)
        self.assertIsNone(state['active'])
        self.assertEqual(state['cycles']['42']['outcome'], facts)
        self.assertEqual(state['cycles']['42']['phase'], 'aborted')
        with self.assertRaises(ReleaseError): authorize_source(state, SHA, ['web'], 999999, allow_closed=True)

    def test_abort_records_unknown_availability_instead_of_claiming_failure_or_blocking_exit(self):
        remote = Mock()
        remote.pages.return_value = []
        with patch('cycle_control.reservations', return_value=[]), \
             patch('cycle_control.baselines', side_effect=ReleaseError('Apple baseline unavailable')):
            outcome = reconcile_abort(remote, {'id': '42', 'source': SHA, 'channels': ['ios-app-store']}, None)
        self.assertEqual(outcome['ios-app-store']['actualState'], 'unknown')
        self.assertIsNone(outcome['ios-app-store']['actualSource'])

    def test_aborted_hotfix_retains_backflow_obligation_without_locking_dev(self):
        state = transition(self.start(), 'hotfix', 'bob', 110, cycle='43', source='b'*40,
                           base=SHA, issue=1241, channels=['web'], note='Outage')
        state = transition(state, 'abort', 'bob', 120, cycle='43', note='Partial failure; retain fix',
                           outcome={'web': {'receiptState': 'failure'}}, needs_backflow=True)
        self.assertIsNone(state['active'])
        with self.assertRaises(ReleaseError):
            transition(state, 'start', 'alice', 130, cycle='44', source='c'*40, issue=1242, channels=['web'])
        state = transition(state, 'backflow', 'bob', 140, cycle='43', source='c'*40, note='Merged and retested')
        self.assertEqual(state['active'], '42')
        self.assertEqual(state['cycles']['43']['phase'], 'aborted')
        self.assertFalse(state['pendingHotfixes'])

    def test_selection_accumulates_same_source_and_advance_drops_old_bindings(self):
        state = self.start(['web', 'android'])
        state = transition(state, 'build', 'alice', 110, cycle='42', run='50', purpose='dev', artifacts=['web'])
        state = transition(state, 'build', 'alice', 111, cycle='42', run='51', purpose='dev', artifacts=['android'])
        self.assertEqual(state['cycles']['42']['previews']['dev'], {'web': '50', 'android': '51'})
        state = transition(state, 'advance', 'alice', 112, cycle='42', source='b'*40, note='Fix')
        self.assertFalse(state['cycles']['42']['previews'])

    def test_aborted_candidate_cannot_borrow_older_closed_qa_for_same_source(self):
        state = self.start()
        state['cycles']['42'].update(phase='closed', frozen=False, qa={'source': SHA})
        state['active'] = None
        state = transition(state, 'start', 'alice', 110, cycle='43', source=SHA, issue=1241, channels=['web'])
        state = transition(state, 'abort', 'alice', 120, cycle='43', note='Bad candidate', outcome={'web': {}})
        with self.assertRaises(ReleaseError): authorize_source(state, SHA, ['web'], 130, allow_closed=True)

    def test_legacy_reserved_release_can_recover_but_cannot_bypass_new_cycle(self):
        manifest = {'tag': 'v1.0.0', 'sourceSha': SHA, 'product': 'web', 'operation': 'release',
                    'candidateId': 'web-v1.0.0', 'channels': ['web']}
        remote = Mock()
        remote.api.side_effect = lambda path, **kw: {'merge_commit_sha': 'b'*40} if path.startswith('pulls/') else {'object': {'type': 'tag'}}
        with patch('cycle_control.load', return_value=empty_state()), patch('cycle_control.fetch_source'), \
             patch('cycle_control.git', return_value=''), patch('controller.verify_reservation') as reserved, \
             patch('cycle_control.reviewed_main_pr') as new_review:
            result = production_gate(remote, SHA, ['web'], manifest=manifest, binding={'pr': 1})
            self.assertIsNone(result)
            reserved.assert_called_once()
            new_review.assert_not_called()
        with patch('cycle_control.load', return_value=empty_state()), patch('cycle_control.fetch_source'), \
             patch('cycle_control.git', return_value='100644 blob controller scripts/release/cycle.py'), \
             patch('controller.verify_reservation'):
            with self.assertRaises(ReleaseError):
                production_gate(remote, SHA, ['web'], manifest=manifest, binding={'pr': 1})


if __name__ == '__main__': unittest.main()
