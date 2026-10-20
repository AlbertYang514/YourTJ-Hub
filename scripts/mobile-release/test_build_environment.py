import unittest
from build_environment import dart_defines
from publish_preview_ios import check_groups


class BuildEnvironmentTests(unittest.TestCase):
    def test_hosts_identity_and_distribution_are_bound_together(self):
        dev = dart_defines('dev', 'a'*40, True)
        production = dart_defines('production', 'a'*40, False)
        self.assertIn('--dart-define=YOURTJ_API_BASE_URL=https://dev.yourtj.de', dev)
        self.assertIn('--dart-define=YOURTJ_OIDC_ISSUER=https://dev.yourtj.de/api/oauth', dev)
        self.assertIn('--dart-define=YOURTJ_PREVIEW=true', dev)
        self.assertIn('--dart-define=YOURTJ_API_BASE_URL=https://f.yourtj.de', production)
        for environment, source, preview in [('dev', 'a'*40, False), ('other', 'a'*40, True), ('dev', 'main', True)]:
            with self.assertRaises(ValueError): dart_defines(environment, source, preview)

    def test_preview_group_cannot_leak_to_automatic_or_external_groups(self):
        groups = [{'id': 'qa', 'attributes': {'isInternalGroup': True, 'hasAccessToAllBuilds': False}},
                  {'id': 'public', 'attributes': {'isInternalGroup': False, 'hasAccessToAllBuilds': False}}]
        check_groups(groups, 'qa')
        with self.assertRaises(ValueError): check_groups(groups, 'public')
        for value in [True, None]:
            groups[1]['attributes']['hasAccessToAllBuilds'] = value
            with self.assertRaises(ValueError): check_groups(groups, 'qa')


if __name__ == '__main__': unittest.main()
