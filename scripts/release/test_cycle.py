import unittest
from cycle import empty_state, transition, can_merge, authorize_source
from model import ReleaseError

SHA = 'a' * 40
NEW = 'b' * 40


class CycleTests(unittest.TestCase):
    def start(self, **extra):
        return transition(empty_state(), 'start', 'alice', 100, cycle='42',
                          source=SHA, issue=1240, channels=['web', 'android', 'ios-testflight'], **extra)

    def change(self, state, action, actor='alice', **kwargs):
        return transition(state, action, actor, 110, cycle='42', **kwargs)

    def test_freeze_excludes_another_candidate_and_stale_pr_head(self):
        state = self.start()
        self.assertFalse(can_merge(state, 12, SHA))
        state = self.change(state, 'admit', pr=12, head=SHA, note='Login blocker')
        self.assertTrue(can_merge(state, 12, SHA))
        self.assertFalse(can_merge(state, 12, NEW))
        with self.assertRaises(ReleaseError):
            transition(state, 'start', 'bob', 110, cycle='43', source=NEW, issue=2, channels=['web'])

    def test_retries_and_handoff_do_not_silently_replace_owner(self):
        state = self.start()
        with self.assertRaises(ReleaseError):
            self.change(state, 'extend', actor='bob', until=500, note='Need device')
        state = self.change(state, 'offer', assignee='bob', note='Handoff')
        self.assertEqual(state['cycles']['42']['owner'], 'alice')
        with self.assertRaises(ReleaseError):
            self.change(state, 'accept', actor='carol', note='Accept')
        state = self.change(state, 'accept', actor='bob', note='Accept')
        self.assertEqual(state['cycles']['42']['owner'], 'bob')

    def test_revision_or_redeployment_invalidates_all_qa(self):
        state = self.start()
        state = self.change(state, 'build', run='500', purpose='dev')
        state = self.change(state, 'qa', note='Device checklist complete', run='500')
        state = self.change(state, 'build', run='501', purpose='production')
        state = self.change(state, 'production-qa', note='Production differences checked', run='501')
        authorize_source(state, SHA, ['web'], 110)
        state = self.change(state, 'advance', source=NEW, note='Fixed login')
        with self.assertRaises(ReleaseError):
            authorize_source(state, NEW, ['web'], 110)
        self.assertIsNone(state['cycles']['42']['qa'])
        self.assertIsNone(state['cycles']['42']['productionQa'])

    def test_timeout_never_unlocks_or_authorizes(self):
        state = self.start(until=120)
        self.assertFalse(can_merge(state, 12, SHA))
        with self.assertRaises(ReleaseError):
            transition(state, 'build', 'alice', 121, cycle='42', run='500', purpose='dev')
        state = transition(state, 'extend', 'alice', 121, cycle='42', until=200, note='Waiting for testers')
        self.assertTrue(state['cycles']['42']['frozen'])

    def test_cancel_releases_dev_but_revokes_qa(self):
        state = self.change(self.start(), 'cancel', note='Abandon candidate')
        self.assertTrue(can_merge(state, 12, SHA))
        with self.assertRaises(ReleaseError):
            authorize_source(state, SHA, ['web'], 110)

    def test_server_can_supply_new_api_before_mobile_production_rehearsal(self):
        state = self.start()
        state['cycles']['42']['qa'] = {'source': SHA}
        authorize_source(state, SHA, ['web'], 110)
        with self.assertRaises(ReleaseError): authorize_source(state, SHA, ['android'], 110)

    def test_hotfix_suspends_normal_candidate_until_backflow_and_new_qa(self):
        state = transition(self.start(), 'hotfix', 'bob', 110, cycle='43', source=NEW,
                           base=SHA, issue=1241, channels=['web'], note='Production outage')
        self.assertEqual(state['cycles']['42']['phase'], 'paused')
        with self.assertRaises(ReleaseError):
            authorize_source(state, SHA, ['web'], 110)
        state['cycles']['43']['qa'] = state['cycles']['43']['productionQa'] = {'source': NEW}
        state['cycles']['43'].update(phase='backflow', completion={'candidate': 'web-1.2.3'})
        self.assertEqual(state['active'], '43')  # Backflow is still outstanding.
        state = transition(state, 'backflow', 'bob', 110, cycle='43', source='c'*40, note='Merged and retested')
        self.assertEqual(state['active'], '42')
        self.assertEqual(state['cycles']['42']['source'], 'c'*40)
        self.assertIsNone(state['cycles']['42']['qa'])
        self.assertIn(NEW, state['requiredHotfixes'])
        self.assertEqual(state['cycles']['43']['phase'], 'closed')

    def test_cancelling_paused_candidate_does_not_resurrect_it_after_hotfix(self):
        state = transition(self.start(), 'hotfix', 'bob', 110, cycle='43', source=NEW,
                           base=SHA, issue=1241, channels=['web'], note='Outage')
        state = self.change(state, 'cancel', note='Cancel the paused release')
        state['cycles']['43']['qa'] = {'source': NEW}
        state['cycles']['43'].update(phase='backflow', completion={'candidate': 'web-1.2.3'})
        state = transition(state, 'backflow', 'bob', 110, cycle='43', source='c'*40, note='Retested merge')
        self.assertIsNone(state['active'])
        self.assertEqual(state['cycles']['42']['phase'], 'cancelled')


if __name__ == '__main__':
    unittest.main()
