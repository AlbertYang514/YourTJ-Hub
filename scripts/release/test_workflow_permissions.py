"""Workflow jobs that re-run release-freeze must hold the token permission to do it."""
from pathlib import Path
import re
import unittest

WORKFLOWS = Path(__file__).resolve().parents[2] / '.github' / 'workflows'
RERUNS_FREEZE = re.compile(r'cycle_control\.py checks\b')


def permissions(lines, indent, default=None):
    """Read a `permissions:` mapping (block or flow style) at the given indentation."""
    key = ' ' * indent + 'permissions:'
    for i, line in enumerate(lines):
        if not line.startswith(key):
            continue
        rest = line[len(key):].strip()
        if rest.startswith('{'):
            pairs = (item.split(':', 1) for item in rest.strip('{} ').split(',') if item.strip())
            return {k.strip(): v.strip() for k, v in pairs}
        if rest:
            return {'*': rest}
        result = {}
        for item in lines[i + 1:]:
            match = re.match(' ' * (indent + 2) + r'([a-z-]+):\s*(\S+)', item)
            if not match:
                break
            result[match.group(1)] = match.group(2)
        return result
    return default or {}


def jobs(text):
    """Split the `jobs:` section into {job id: lines} by its two-space job keys."""
    if '\njobs:\n' not in text:
        return {}
    result, name = {}, None
    for line in text.split('\njobs:\n', 1)[1].splitlines():
        match = re.match(r'  ([A-Za-z0-9_-]+):\s*$', line)
        if match:
            name = match.group(1)
            result[name] = []
        elif line and not line.startswith(' '):
            break
        elif name and line.startswith('    '):
            result[name].append(line)
    return result


class FreezeRerunPermissions(unittest.TestCase):
    def test_every_job_that_reruns_release_freeze_can_write_actions(self):
        checked = []
        for path in sorted(WORKFLOWS.glob('*.y*ml')):
            text = path.read_text(encoding='utf-8')
            if not RERUNS_FREEZE.search(text):
                continue
            default = permissions(text.splitlines(), 0)
            for name, lines in jobs(text).items():
                if not any(RERUNS_FREEZE.search(line) for line in lines):
                    continue
                checked.append(f'{path.name}:{name}')
                granted = permissions(lines, 4, default)
                with self.subTest(job=checked[-1]):
                    self.assertIn(granted.get('actions', granted.get('*')), ('write', 'write-all'),
                                  'cycle_control.py checks re-runs release-freeze and needs actions: write')
        self.assertIn('release-prerelease.yml:begin', checked)
        self.assertIn('release-control.yml:project', checked)


if __name__ == '__main__':
    unittest.main()
