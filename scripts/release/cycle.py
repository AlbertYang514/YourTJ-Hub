"""Release ownership and QA protocol. Pure transitions; no credentials or side effects.

The trusted controller supplies verified GitHub/Apple facts before applying a transition.
An expired freeze remains a freeze. A new source or dev database reset revokes QA.
"""
from copy import deepcopy
import re
from model import SHA, FILES, require


def empty_state():
    return {'schema': 1, 'active': None, 'cycles': {}, 'requiredHotfixes': [], 'pendingHotfixes': {}}


def current(state):
    return state['cycles'].get(state['active'])


def live(cycle, now):
    require(cycle['phase'] not in {'cancelled', 'closed', 'paused', 'aborted'}, 'Cycle is not active')
    require(not cycle['frozen'] or now < cycle['until'], 'Freeze expired: explicitly extend or cancel')


def invalidate(cycle):
    cycle.update(qa=None, productionQa=None, builds={}, previews={}, admitted={}, phase='testing')


def preview_artifacts(value, purpose):
    choices = ['web', 'android', 'ios'] if purpose == 'dev' else ['android', 'ios']
    artifacts = choices if not value else value.split(',') if isinstance(value, str) else value
    require(artifacts and len(artifacts) == len(set(artifacts)) and set(artifacts) <= set(choices),
            'Select web/android/ios for dev; android/ios for production rehearsal')
    return artifacts


def required_artifacts(channels, purpose):
    return ({'web'} if purpose == 'dev' else set()) | ({'android'} if 'android' in channels else set()) | (
        {'ios'} if any(c.startswith('ios-') for c in channels) else set())


def resume_parent(state, cycle, now, source=None):
    parent_id = cycle.get('paused')
    parent = state['cycles'].get(parent_id)
    if parent and parent['phase'] == 'paused':
        invalidate(parent)
        parent['until'] = now + 86400
        if source:
            parent['source'] = source
        state['active'] = parent_id
    else:
        state['active'] = None


def can_merge(state, pr, head, now=None):
    active = current(state)
    if active and active['frozen'] and now is not None and now >= active['until']:
        return False
    return not active or not active['frozen'] or active['admitted'].get(str(pr)) == head


def authorize_source(state, source, channels, now, *, allow_closed=False):
    matches = [c for c in state['cycles'].values() if c['source'] == source
               and set(channels) <= set(c['channels'])]
    require(matches, 'Source/targets have no release cycle')
    cycle = matches[-1]
    require(cycle['phase'] in ({'testing', 'backflow', 'closed'} if allow_closed else {'testing'}),
            'The latest cycle for this source ended or was paused; older QA cannot authorize it')
    require(cycle['kind'] == 'hotfix' or not state.get('pendingHotfixes'), 'Complete outstanding hotfix backflow first')
    # A hotfix preempts ordinary publication until its source flows back to dev.
    active = current(state)
    require(not active or active['kind'] != 'hotfix' or active['id'] == cycle['id'],
            'Hotfix owns publication; ordinary candidates are paused')
    require(not cycle['frozen'] or now < cycle['until'], 'Freeze expired: extend or cancel')
    required_qa = ['qa'] + (['productionQa'] if any(c != 'web' for c in channels) else [])
    require(all(cycle[key] and cycle[key]['source'] == source for key in required_qa),
            'Exact source needs dev QA; mobile publication also needs production-configuration QA')
    return cycle


def transition(state, action, actor, now, *, cycle, **data):
    state = deepcopy(state)
    require(isinstance(cycle, str) and re.fullmatch(r'[1-9]\d*', cycle), 'Cycle ID must be an Actions run ID')
    note = data.get('note', '').strip()
    require(isinstance(actor, str) and actor and not actor.endswith('[bot]'), 'A human owner is required')
    if action in {'start', 'hotfix'}:
        source, channels = data.get('source'), data.get('channels', [])
        require(isinstance(source, str) and SHA.fullmatch(source), 'Full source SHA required')
        require(channels and len(channels) == len(set(channels)) and set(channels) <= FILES.keys(), 'Invalid targets')
        require(type(data.get('issue')) is int and data['issue'] > 0, 'Release issue required')
        require(cycle not in state['cycles'], 'Cycle already exists; retry its build, not its initiation')
        require(not state.get('pendingHotfixes'), 'Complete outstanding hotfix backflow first')
        active = current(state)
        require(not active or action == 'hotfix' and active['kind'] == 'normal', 'Another cycle owns dev')
        if action == 'hotfix':
            require(note and SHA.fullmatch(data.get('base', '')), 'Hotfix needs deployed base and reason')
        if active:
            active['phase'] = 'paused'
            active['qa'] = active['productionQa'] = None
        until = data.get('until') or now + 86400
        require(now < until <= now + 7 * 86400, 'Freeze deadline must be within seven days')
        state['cycles'][cycle] = {
            'id': cycle, 'owner': actor, 'offeredTo': None, 'source': source, 'issue': data['issue'],
            'channels': channels, 'kind': 'hotfix' if action == 'hotfix' else 'normal',
            'base': data.get('base'), 'paused': active['id'] if active else None,
            'phase': 'testing', 'frozen': True, 'started': now, 'until': until,
            'admitted': {}, 'qa': None, 'productionQa': None, 'builds': {}, 'previews': {},
        }
        state['active'] = cycle
    else:
        require(cycle in state['cycles'], 'Unknown cycle')
        c = state['cycles'][cycle]
        require(c['phase'] not in {'closed', 'cancelled'}, 'Cycle already ended')
        if action == 'reassign':
            require(data.get('admin') is True and note and data.get('assignee'), 'Repository admin and reassignment reason required')
            require(data['assignee'] != c['owner'], 'Choose a different release owner')
            c.setdefault('ownershipHistory', []).append({'previous': c['owner'], 'owner': data['assignee'],
                                                        'actor': actor, 'at': now, 'note': note})
            c.update(owner=data['assignee'], offeredTo=None)
        elif action == 'accept':
            require(actor == c['offeredTo'] and note, 'Only the offered owner can accept a handoff')
            c.update(owner=actor, offeredTo=None)
        else:
            require(actor == c['owner'], 'Only the release owner can change this cycle')
            require(c['phase'] != 'aborted' or action in {'offer', 'backflow'}, 'Aborted publication cannot resume')
            require(note or action == 'build', 'A reason or QA summary is required')
            if action == 'offer':
                require(data.get('assignee') and data['assignee'] != actor, 'Name the receiving owner')
                c['offeredTo'] = data['assignee']
            elif action == 'extend':
                require(c['frozen'] and now < data.get('until', 0) <= now + 7 * 86400, 'Invalid freeze extension')
                c['until'] = data['until']
            elif action == 'cancel':
                require(not c.get('completion'), 'Published cycle must finish backflow, not cancel')
                c.update(phase='cancelled', frozen=False, qa=None, productionQa=None)
                if state['active'] == cycle:
                    resume_parent(state, c, now)
                elif current(state) and current(state)['paused'] == cycle:
                    current(state)['paused'] = None
            elif action == 'abort':
                require(isinstance(data.get('outcome'), dict) and set(data['outcome']) == set(c['channels']),
                        'Reconcile every selected production channel before aborting')
                c.update(phase='aborted', frozen=False, qa=None, productionQa=None, admitted={},
                         outcome=data['outcome'], abortedAt=now, abortReason=note)
                pending = c['kind'] == 'hotfix' and data.get('needs_backflow') is True and not c.get('backflow')
                if pending:
                    state.setdefault('pendingHotfixes', {})[cycle] = c['source']
                if state['active'] == cycle:
                    state['active'] = None
                    if not pending:
                        resume_parent(state, c, now)
                elif current(state) and current(state)['paused'] == cycle:
                    current(state)['paused'] = None
            elif action == 'backflow' and c['phase'] == 'aborted':
                require(cycle in state.get('pendingHotfixes', {}) and not current(state), 'No pending backflow or another cycle owns dev')
                require(SHA.fullmatch(data.get('source', '')), 'Full merged dev SHA required')
                state['requiredHotfixes'].append(c['source'])
                del state['pendingHotfixes'][cycle]
                c['backflow'] = data['source']
                resume_parent(state, c, now, data['source'])
            elif action == 'backflow':
                require(c['kind'] == 'hotfix' and c.get('completion'), 'Hotfix publication must complete before backflow')
                require(SHA.fullmatch(data.get('source', '')), 'Full merged dev SHA required')
                state['requiredHotfixes'].append(c['source'])
                c.update(frozen=False, phase='closed', backflow=data['source'])
                resume_parent(state, c, now, data['source'])
            else:
                live(c, now)
                if action == 'admit':
                    require(state['active'] == cycle and c['frozen'], 'Only the dev owner admits fixes')
                    require(type(data.get('pr')) is int and SHA.fullmatch(data.get('head', '')), 'Exact PR head required')
                    c['admitted'][str(data['pr'])] = data['head']
                elif action == 'advance':
                    require(c['frozen'] and state['active'] == cycle, 'Only the frozen candidate can advance')
                    require(not c.get('completion'), 'A deployed hotfix must flow back before changing its source')
                    require(SHA.fullmatch(data.get('source', '')), 'Full source SHA required')
                    c['source'] = data['source']
                    invalidate(c)
                elif action == 'build':
                    require(c['frozen'] and state['active'] == cycle, 'Only the frozen candidate can build')
                    require(not c.get('completion'), 'Complete hotfix backflow before another build')
                    purpose = data.get('purpose')
                    require(purpose in {'dev', 'production'}, 'Unknown build environment')
                    artifacts = preview_artifacts(data.get('artifacts'), purpose)
                    require(str(data.get('run', '')).isdigit(), 'Build run ID required')
                    if purpose == 'dev':
                        previous = c.get('previews', {}).get('dev', {}).copy()
                        invalidate(c)
                        c['previews']['dev'] = previous
                    else:
                        require(c['qa'] and c['qa']['source'] == c['source'], 'Dev QA must pass first')
                        c['productionQa'] = None
                    c['builds'][purpose] = str(data['run'])
                    c.setdefault('previews', {}).setdefault(purpose, {}).update({a: str(data['run']) for a in artifacts})
                elif action in {'qa', 'production-qa'}:
                    purpose = 'dev' if action == 'qa' else 'production'
                    require(c['builds'].get(purpose) == str(data.get('run')), 'QA does not match current build run')
                    c['qa' if action == 'qa' else 'productionQa'] = {
                        'source': c['source'], 'actor': actor, 'note': note, 'run': str(data['run']), 'at': now,
                        'artifacts': c.get('previews', {}).get(purpose, {}).copy()}
                else:
                    require(False, 'Unknown cycle action')
    state['lastEvent'] = {'action': action, 'cycle': cycle, 'actor': actor, 'at': now, 'note': note}
    return state
