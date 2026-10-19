import unittest
from cycle import empty_state, transition
from preview_status import project, notes


class PreviewStatusTests(unittest.TestCase):
    def test_public_projection_omits_private_facts_and_tracks_source_lifecycle(self):
        state = transition(empty_state(), 'start', 'alice', 100, cycle='42', source='a'*40, issue=1240, channels=['web'])
        state['cycles']['42']['qa'] = {'note': 'private details'}
        state['cycles']['42']['outcome'] = {'secret': 'Apple metadata'}
        status = project(state, 110)
        self.assertEqual(status['active']['source'], 'a'*40)
        self.assertNotIn('private', str(status))
        self.assertNotIn('Apple', str(status))
        state = transition(state, 'advance', 'alice', 120, cycle='42', source='b'*40, note='Fix')
        self.assertEqual(project(state, 121)['active']['source'], 'b'*40)
        state = transition(state, 'abort', 'alice', 130, cycle='42', note='Failed', outcome={'web': {}})
        self.assertIsNone(project(state, 131)['active'])
        self.assertIn('no active preview', notes(project(state, 131), 'YourTongji/YourTJ-Hub'))


if __name__ == '__main__': unittest.main()
